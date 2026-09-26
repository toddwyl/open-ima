# P1: Parser Sidecar 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付独立的 Python 文档解析服务,支持 pdf/docx/pptx/md/txt/html 六类文件,通过 `POST /parse` 返回结构化 block 序列。

**Architecture:** FastAPI 单进程。`POST /parse` 接收文件 URL → fetcher 拉取字节流 → registry 按 file_type 路由到对应 Parser → 返回 `(title, blocks)`。Parser 为插件注册表模式,新增类型零改动路由代码。不下载任何模型权重。

**Tech Stack:** Python 3.11, FastAPI, uvicorn, httpx, pypdf, python-docx, python-pptx, markdown-it-py, beautifulsoup4, readability-lxml, pytest, fpdf2(测试夹具)

**Spec:** [design/2026-09-26-open-ima-v1-design.md](../../../design/2026-09-26-open-ima-v1-design.md) §4(独立解析层)、§12(docling 演进路径)

**Roadmap:** [2026-09-26-roadmap.md](2026-09-26-roadmap.md)(含本服务对外的完整接口契约)

## Global Constraints

- 全部代码在 `parser/` 子目录;不触碰仓库其他部分
- 禁止引入需要下载模型权重的库(无 docling/unstructured/torch)
- 所有解析器对损坏/加密输入抛 `ParseFailure`(→422),不吞异常返回空结果
- 测试不许访问外网;样例文件全部由夹具在 tmp_path 生成
- 每个 Task 结束提交一次,commit message 以 `feat(parser):` 或 `test(parser):` 开头

## File Structure

```
parser/
├── requirements.txt           # 运行依赖
├── requirements-dev.txt       # pytest, fpdf2
├── Dockerfile
├── app/
│   ├── __init__.py
│   ├── main.py                # FastAPI 入口:GET /health, POST /parse, 异常→HTTP 映射
│   ├── models.py              # ParseRequest / ParseResponse / Block / ParseError
│   ├── errors.py              # ParseFailure(内容不可解析), FetchFailure(拉取失败)
│   ├── fetcher.py             # 按 URL 拉取文件,超时 30s,上限 50MB
│   ├── registry.py            # PARSERS 注册表 + get_parser()
│   └── parsers/
│       ├── __init__.py
│       ├── base.py            # Parser Protocol(结构类型,非 ABC)
│       ├── text_parser.py     # txt
│       ├── markdown_parser.py # md
│       ├── html_parser.py     # html(readability 抽正文)
│       ├── pdf_parser.py      # pypdf
│       ├── docx_parser.py     # python-docx
│       └── pptx_parser.py     # python-pptx
└── tests/
    ├── conftest.py            # 样例文件夹具(txt/md/html/docx/pptx/pdf)
    ├── test_text_parser.py
    ├── test_markdown_parser.py
    ├── test_html_parser.py
    ├── test_pdf_parser.py
    ├── test_docx_parser.py
    ├── test_pptx_parser.py
    ├── test_registry.py
    └── test_api.py            # /health、/parse 端到端(monkeypatch fetcher)
```

---

### Task 1: 项目骨架 + models/errors + text parser

**Files:**
- Create: `parser/requirements.txt`, `parser/requirements-dev.txt`
- Create: `parser/app/__init__.py`(空文件)
- Create: `parser/app/models.py`
- Create: `parser/app/errors.py`
- Create: `parser/app/parsers/__init__.py`(空文件)
- Create: `parser/app/parsers/base.py`
- Create: `parser/app/parsers/text_parser.py`
- Test: `parser/tests/test_text_parser.py`

**Interfaces:**
- Produces(P1 全部后续 Task 依赖):
  - `Block(BaseModel)`: 字段 `type: str`(`"heading"|"paragraph"|"list"|"table"`)、`text: str`、`level: int = 0`(非 heading 恒为 0)
  - `ParseRequest(BaseModel)`: `file_url: str`, `file_type: str`
  - `ParseResponse(BaseModel)`: `title: str`, `blocks: list[Block]`
  - `ParseError(BaseModel)`: `error: str`
  - `errors.ParseFailure(Exception)`、`errors.FetchFailure(Exception)`
  - Parser 协议:`parse(self, data: bytes, filename: str) -> tuple[str, list[Block]]`,返回 `(title, blocks)`

- [ ] **Step 1: 写依赖文件**

`parser/requirements.txt`:
```
fastapi>=0.115
uvicorn[standard]>=0.30
httpx>=0.27
pypdf>=4.0
python-docx>=1.1
python-pptx>=1.0
markdown-it-py>=3.0
beautifulsoup4>=4.12
readability-lxml>=0.8.1
lxml>=5.0
```

`parser/requirements-dev.txt`:
```
-r requirements.txt
pytest>=8.0
fpdf2>=2.7
```

安装:`cd parser && python -m venv .venv && source .venv/bin/activate && pip install -r requirements-dev.txt`

- [ ] **Step 2: 写失败测试**

`parser/tests/test_text_parser.py`:
```python
from app.parsers.text_parser import TextParser


def test_text_parser_splits_paragraphs():
    data = "第一段内容。\n\n第二段内容。\n\n\n第三段内容。".encode("utf-8")
    title, blocks = TextParser().parse(data, "notes.txt")
    assert title == "notes"
    assert [b.type for b in blocks] == ["paragraph", "paragraph", "paragraph"]
    assert blocks[0].text == "第一段内容。"
    assert blocks[2].text == "第三段内容。"


def test_text_parser_strips_blank_paragraphs():
    title, blocks = TextParser().parse("  \n\n  \n\n只有一段 ".encode(), "a.txt")
    assert len(blocks) == 1
    assert blocks[0].text == "只有一段"


def test_text_parser_handles_non_utf8_bytes():
    title, blocks = TextParser().parse("中文".encode("gbk"), "gbk.txt")
    assert len(blocks) == 1  # errors="replace" 不抛异常
```

- [ ] **Step 3: 跑测试确认失败**

Run: `cd parser && python -m pytest tests/test_text_parser.py -v`
Expected: FAIL,`ModuleNotFoundError: No module named 'app'`

- [ ] **Step 4: 实现 models / errors / base / text_parser**

`parser/app/models.py`:
```python
from pydantic import BaseModel


class ParseRequest(BaseModel):
    file_url: str
    file_type: str


class Block(BaseModel):
    type: str  # heading | paragraph | list | table
    text: str
    level: int = 0  # heading 层级 1-6,其他类型恒为 0


class ParseResponse(BaseModel):
    title: str
    blocks: list[Block]


class ParseError(BaseModel):
    error: str
```

`parser/app/errors.py`:
```python
class ParseFailure(Exception):
    """文件内容不可解析(加密/损坏/无文本层)。HTTP 422,不可重试。"""


class FetchFailure(Exception):
    """按 URL 拉取文件失败(网络/超时/超限)。HTTP 502,可重试。"""
```

`parser/app/parsers/base.py`:
```python
from typing import Protocol

from app.models import Block


class Parser(Protocol):
    """结构化解析器协议:字节流 -> (标题, block 序列)。

    实现约束:
    - 不抛未捕获异常;内容不可解析时抛 errors.ParseFailure
    - 不返回空 blocks(无内容应抛 ParseFailure)
    """

    def parse(self, data: bytes, filename: str) -> tuple[str, list[Block]]: ...
```

`parser/app/parsers/text_parser.py`:
```python
from app.models import Block


class TextParser:
    def parse(self, data: bytes, filename: str) -> tuple[str, list[Block]]:
        text = data.decode("utf-8", errors="replace")
        blocks = [
            Block(type="paragraph", text=para.strip())
            for para in text.split("\n\n")
            if para.strip()
        ]
        title = filename.rsplit(".", 1)[0]
        return title, blocks
```

- [ ] **Step 5: 跑测试确认通过**

Run: `cd parser && python -m pytest tests/test_text_parser.py -v`
Expected: 3 passed

- [ ] **Step 6: Commit**

```bash
git add parser/
git commit -m "feat(parser): scaffold with models, errors and text parser"
```

---

### Task 2: markdown parser

**Files:**
- Create: `parser/app/parsers/markdown_parser.py`
- Test: `parser/tests/test_markdown_parser.py`

**Interfaces:**
- Consumes: Task 1 的 `Block`、`Parser` 协议
- Produces: `MarkdownParser`,首个 H1 作为 title;heading 带 level;列表合并为单个 `list` block(每行一项)

- [ ] **Step 1: 写失败测试**

`parser/tests/test_markdown_parser.py`:
```python
from app.parsers.markdown_parser import MarkdownParser

MD = """# 文档标题

开头段落。

## 第二章

- 要点一
- 要点二

1. 第一
2. 第二

结尾段落。
"""


def test_markdown_blocks_and_levels():
    title, blocks = MarkdownParser().parse(MD.encode(), "doc.md")
    assert title == "文档标题"
    types = [(b.type, b.level) for b in blocks]
    assert types == [
        ("heading", 1),
        ("paragraph", 0),
        ("heading", 2),
        ("list", 0),
        ("list", 0),
        ("paragraph", 0),
    ]
    assert blocks[3].text == "要点一\n要点二"
    assert blocks[4].text == "第一\n第二"


def test_markdown_title_falls_back_to_filename():
    title, _ = MarkdownParser().parse("只有段落。".encode(), "noheading.md")
    assert title == "noheading"
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd parser && python -m pytest tests/test_markdown_parser.py -v`
Expected: FAIL,`ModuleNotFoundError`

- [ ] **Step 3: 实现**

`parser/app/parsers/markdown_parser.py`:
```python
from markdown_it import MarkdownIt

from app.models import Block


class MarkdownParser:
    def parse(self, data: bytes, filename: str) -> tuple[str, list[Block]]:
        text = data.decode("utf-8", errors="replace")
        tokens = MarkdownIt().parse(text)
        blocks: list[Block] = []
        title = ""
        i = 0
        while i < len(tokens):
            t = tokens[i]
            if t.type == "heading_open":
                level = int(t.tag[1])
                content = tokens[i + 1].content.strip()
                blocks.append(Block(type="heading", text=content, level=level))
                if level == 1 and not title:
                    title = content
                i += 3  # heading_open + inline + heading_close
            elif t.type == "paragraph_open":
                content = tokens[i + 1].content.strip()
                if content:
                    blocks.append(Block(type="paragraph", text=content))
                i += 3
            elif t.type in ("bullet_list_open", "ordered_list_open"):
                close = "bullet_list_close" if t.type == "bullet_list_open" else "ordered_list_close"
                items: list[str] = []
                i += 1
                while i < len(tokens) and tokens[i].type != close:
                    if tokens[i].type == "inline":
                        items.append(tokens[i].content.strip())
                    i += 1
                blocks.append(Block(type="list", text="\n".join(items)))
                i += 1  # 跳过 close
            else:
                i += 1
        return title or filename.rsplit(".", 1)[0], blocks
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd parser && python -m pytest tests/test_markdown_parser.py -v`
Expected: 2 passed

- [ ] **Step 5: Commit**

```bash
git add parser/app/parsers/markdown_parser.py parser/tests/test_markdown_parser.py
git commit -m "feat(parser): add markdown parser"
```

---

### Task 3: html parser(readability 抽正文)

**Files:**
- Create: `parser/app/parsers/html_parser.py`
- Test: `parser/tests/test_html_parser.py`

**Interfaces:**
- Consumes: Task 1 的 `Block`
- Produces: `HtmlParser`;title 取 readability 提取的标题;正文按 DOM 顺序输出 heading/paragraph/list/table block,忽略 script/style/nav/footer

- [ ] **Step 1: 写失败测试**

`parser/tests/test_html_parser.py`:
```python
from app.parsers.html_parser import HtmlParser

HTML = """<!DOCTYPE html>
<html><head><title>测试页面标题</title>
<style>body{color:red}</style></head>
<body>
<nav>导航链接,不应出现</nav>
<article>
<h1>文章主标题</h1>
<p>第一段正文,长度要足够长以通过 readability 的正文判定,所以需要多写一些内容才行。</p>
<h2>小节标题</h2>
<p>第二段正文,同样写得足够长,保证 readability-lxml 把它当成真正的正文区域而不是杂讯。</p>
<ul><li>列表项甲</li><li>列表项乙</li></ul>
</article>
<footer>页脚信息,不应出现</footer>
<script>console.log("x")</script>
</body></html>
"""


def test_html_extracts_article_blocks():
    title, blocks = HtmlParser().parse(HTML.encode(), "page.html")
    assert title  # readability 提取到非空标题
    texts = [b.text for b in blocks]
    assert any("文章主标题" in t for t in texts)
    assert any("第一段正文" in t for t in texts)
    assert any("列表项甲" in t for t in texts)
    assert not any("导航链接" in t for t in texts)
    assert not any("页脚信息" in t for t in texts)


def test_html_heading_levels():
    _, blocks = HtmlParser().parse(HTML.encode(), "page.html")
    headings = {b.text: b.level for b in blocks if b.type == "heading"}
    assert headings.get("文章主标题") == 1
    assert headings.get("小节标题") == 2
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd parser && python -m pytest tests/test_html_parser.py -v`
Expected: FAIL,`ModuleNotFoundError`

- [ ] **Step 3: 实现**

`parser/app/parsers/html_parser.py`:
```python
from bs4 import BeautifulSoup, Tag
from readability import Document

from app.errors import ParseFailure
from app.models import Block

_HEADING_TAGS = {"h1": 1, "h2": 2, "h3": 3, "h4": 4, "h5": 5, "h6": 6}


class HtmlParser:
    def parse(self, data: bytes, filename: str) -> tuple[str, list[Block]]:
        html = data.decode("utf-8", errors="replace")
        doc = Document(html)
        title = (doc.title() or "").strip() or filename.rsplit(".", 1)[0]
        soup = BeautifulSoup(doc.summary(), "lxml")
        blocks: list[Block] = []
        for el in soup.find_all([*_HEADING_TAGS, "p", "ul", "ol", "table"]):
            block = self._to_block(el)
            if block is not None:
                blocks.append(block)
        if not blocks:
            raise ParseFailure("html: no readable content extracted")
        return title, blocks

    def _to_block(self, el: Tag) -> Block | None:
        text = el.get_text(" ", strip=True)
        if not text:
            return None
        if el.name in _HEADING_TAGS:
            return Block(type="heading", text=text, level=_HEADING_TAGS[el.name])
        if el.name in ("ul", "ol"):
            items = [li.get_text(" ", strip=True) for li in el.find_all("li", recursive=False)]
            return Block(type="list", text="\n".join(i for i in items if i))
        if el.name == "table":
            rows = [
                " | ".join(cell.get_text(" ", strip=True) for cell in tr.find_all(["th", "td"]))
                for tr in el.find_all("tr")
            ]
            return Block(type="table", text="\n".join(r for r in rows if r.strip(" |")))
        return Block(type="paragraph", text=text)
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd parser && python -m pytest tests/test_html_parser.py -v`
Expected: 2 passed。若 readability 因正文太短丢块,把测试 HTML 段落再加长(readability 按文本密度打分)

- [ ] **Step 5: Commit**

```bash
git add parser/app/parsers/html_parser.py parser/tests/test_html_parser.py
git commit -m "feat(parser): add html parser with readability extraction"
```

---

### Task 4: pdf parser + 测试夹具

**Files:**
- Create: `parser/app/parsers/pdf_parser.py`
- Create: `parser/tests/conftest.py`
- Test: `parser/tests/test_pdf_parser.py`

**Interfaces:**
- Consumes: Task 1 的 `Block`、`ParseFailure`
- Produces:
  - `PdfParser`;加密或全文 <20 字符(疑似扫描件)抛 `ParseFailure`
  - conftest 夹具:`sample_pdf(tmp_path_factory) -> bytes`(fpdf2 生成,含两行文本)、`sample_txt` / `sample_md` / `sample_html` 同理返回 bytes

- [ ] **Step 1: 写夹具与失败测试**

`parser/tests/conftest.py`:
```python
import pytest
from fpdf import FPDF


@pytest.fixture(scope="session")
def sample_pdf() -> bytes:
    pdf = FPDF()
    pdf.add_page()
    pdf.set_font("helvetica", size=12)
    pdf.multi_cell(0, 10, "First paragraph of the pdf document.")
    pdf.multi_cell(0, 10, "Second paragraph of the pdf document.")
    return bytes(pdf.output())
```

`parser/tests/test_pdf_parser.py`:
```python
import pytest

from app.errors import ParseFailure
from app.parsers.pdf_parser import PdfParser


def test_pdf_extracts_text(sample_pdf):
    title, blocks = PdfParser().parse(sample_pdf, "report.pdf")
    assert title == "report"
    assert len(blocks) >= 1
    joined = "\n".join(b.text for b in blocks)
    assert "First paragraph" in joined
    assert "Second paragraph" in joined


def test_pdf_rejects_garbage():
    with pytest.raises(ParseFailure):
        PdfParser().parse(b"this is not a pdf at all", "bad.pdf")


def test_pdf_rejects_scanned_like_content():
    # fpdf 生成无文字空白页 -> 文本量低于阈值,判定疑似扫描件
    from fpdf import FPDF
    pdf = FPDF()
    pdf.add_page()
    with pytest.raises(ParseFailure, match="no extractable text"):
        PdfParser().parse(bytes(pdf.output()), "blank.pdf")
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd parser && python -m pytest tests/test_pdf_parser.py -v`
Expected: FAIL,`ModuleNotFoundError`

- [ ] **Step 3: 实现**

`parser/app/parsers/pdf_parser.py`:
```python
import io

from pypdf import PdfReader
from pypdf.errors import PdfReadError

from app.errors import ParseFailure
from app.models import Block

_MIN_TEXT_CHARS = 20  # 低于此值判定为扫描件/无文本层


class PdfParser:
    def parse(self, data: bytes, filename: str) -> tuple[str, list[Block]]:
        try:
            reader = PdfReader(io.BytesIO(data))
        except PdfReadError as exc:
            raise ParseFailure(f"pdf: corrupted file: {exc}") from exc
        if reader.is_encrypted:
            raise ParseFailure("pdf: encrypted file is not supported")
        pages_text: list[str] = []
        for page in reader.pages:
            pages_text.append(page.extract_text() or "")
        full = "\n\n".join(pages_text)
        if len(full.strip()) < _MIN_TEXT_CHARS:
            raise ParseFailure("pdf: no extractable text (scanned document?)")
        blocks = [
            Block(type="paragraph", text=para.strip())
            for para in full.split("\n\n")
            if para.strip()
        ]
        meta_title = (reader.metadata.title or "") if reader.metadata else ""
        title = meta_title.strip() or filename.rsplit(".", 1)[0]
        return title, blocks
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd parser && python -m pytest tests/test_pdf_parser.py -v`
Expected: 3 passed

- [ ] **Step 5: Commit**

```bash
git add parser/app/parsers/pdf_parser.py parser/tests/conftest.py parser/tests/test_pdf_parser.py
git commit -m "feat(parser): add pdf parser with encrypted/scanned detection"
```

---

### Task 5: docx parser

**Files:**
- Modify: `parser/tests/conftest.py`(追加 `sample_docx` 夹具)
- Create: `parser/app/parsers/docx_parser.py`
- Test: `parser/tests/test_docx_parser.py`

**Interfaces:**
- Consumes: Task 1 的 `Block`
- Produces: `DocxParser`;`Heading N` 样式 → heading(level=N);表格 → table block(行内单元格 ` | ` 连接);title 取首个 Heading 1,否则文件名

- [ ] **Step 1: 追加夹具并写失败测试**

`parser/tests/conftest.py` 追加:
```python
@pytest.fixture(scope="session")
def sample_docx() -> bytes:
    import io
    from docx import Document

    doc = Document()
    doc.add_heading("DOCX 主标题", level=1)
    doc.add_paragraph("DOCX 第一段正文。")
    doc.add_heading("DOCX 小节", level=2)
    doc.add_paragraph("DOCX 第二段正文。")
    buf = io.BytesIO()
    doc.save(buf)
    return buf.getvalue()
```

`parser/tests/test_docx_parser.py`:
```python
from app.parsers.docx_parser import DocxParser


def test_docx_headings_and_paragraphs(sample_docx):
    title, blocks = DocxParser().parse(sample_docx, "memo.docx")
    assert title == "DOCX 主标题"
    types = [(b.type, b.level) for b in blocks]
    assert types == [
        ("heading", 1),
        ("paragraph", 0),
        ("heading", 2),
        ("paragraph", 0),
    ]


def test_docx_title_fallback(sample_docx):
    import io
    from docx import Document

    doc = Document()
    doc.add_paragraph("没有标题,只有段落。")
    buf = io.BytesIO()
    doc.save(buf)
    title, blocks = DocxParser().parse(buf.getvalue(), "plain.docx")
    assert title == "plain"
    assert blocks[0].text == "没有标题,只有段落。"
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd parser && python -m pytest tests/test_docx_parser.py -v`
Expected: FAIL,`ModuleNotFoundError`

- [ ] **Step 3: 实现**

`parser/app/parsers/docx_parser.py`:
```python
import io

from docx import Document
from docx.table import Table
from docx.text.paragraph import Paragraph

from app.models import Block


class DocxParser:
    def parse(self, data: bytes, filename: str) -> tuple[str, list[Block]]:
        doc = Document(io.BytesIO(data))
        blocks: list[Block] = []
        title = ""
        for el in doc.element.body:
            if el.tag.endswith("}p"):
                block = self._paragraph(Paragraph(el, doc))
            elif el.tag.endswith("}tbl"):
                block = self._table(Table(el, doc))
            else:
                continue
            if block is None:
                continue
            if block.type == "heading" and block.level == 1 and not title:
                title = block.text
            blocks.append(block)
        return title or filename.rsplit(".", 1)[0], blocks

    def _paragraph(self, p: Paragraph) -> Block | None:
        text = p.text.strip()
        if not text:
            return None
        style = p.style.name or ""
        if style.startswith("Heading"):
            try:
                level = int(style.rsplit(" ", 1)[1])
            except (IndexError, ValueError):
                level = 1
            return Block(type="heading", text=text, level=level)
        if style.startswith("List"):
            return Block(type="list", text=text)
        return Block(type="paragraph", text=text)

    def _table(self, t: Table) -> Block | None:
        rows = [
            " | ".join(cell.text.strip() for cell in row.cells)
            for row in t.rows
        ]
        text = "\n".join(r for r in rows if r.strip(" |"))
        return Block(type="table", text=text) if text else None
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd parser && python -m pytest tests/test_docx_parser.py -v`
Expected: 2 passed

- [ ] **Step 5: Commit**

```bash
git add parser/
git commit -m "feat(parser): add docx parser"
```

---

### Task 6: pptx parser

**Files:**
- Modify: `parser/tests/conftest.py`(追加 `sample_pptx` 夹具)
- Create: `parser/app/parsers/pptx_parser.py`
- Test: `parser/tests/test_pptx_parser.py`

**Interfaces:**
- Consumes: Task 1 的 `Block`
- Produces: `PptxParser`;每页 slide 的标题占位符 → heading(level=1),其余文本框 → paragraph;title 取第一页标题,否则文件名

- [ ] **Step 1: 追加夹具并写失败测试**

`parser/tests/conftest.py` 追加:
```python
@pytest.fixture(scope="session")
def sample_pptx() -> bytes:
    import io
    from pptx import Presentation

    prs = Presentation()
    slide = prs.slides.add_slide(prs.slide_layouts[0])  # 标题页
    slide.shapes.title.text = "PPTX 演示标题"
    slide.placeholders[1].text = "副标题内容"
    slide2 = prs.slides.add_slide(prs.slide_layouts[1])  # 标题+内容页
    slide2.shapes.title.text = "第二页标题"
    slide2.placeholders[1].text = "第二页正文要点"
    buf = io.BytesIO()
    prs.save(buf)
    return buf.getvalue()
```

`parser/tests/test_pptx_parser.py`:
```python
from app.parsers.pptx_parser import PptxParser


def test_pptx_slides_to_blocks(sample_pptx):
    title, blocks = PptxParser().parse(sample_pptx, "deck.pptx")
    assert title == "PPTX 演示标题"
    headings = [b for b in blocks if b.type == "heading"]
    assert [h.text for h in headings] == ["PPTX 演示标题", "第二页标题"]
    paras = [b.text for b in blocks if b.type == "paragraph"]
    assert any("副标题内容" in t for t in paras)
    assert any("第二页正文要点" in t for t in paras)
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd parser && python -m pytest tests/test_pptx_parser.py -v`
Expected: FAIL,`ModuleNotFoundError`

- [ ] **Step 3: 实现**

`parser/app/parsers/pptx_parser.py`:
```python
import io

from pptx import Presentation

from app.errors import ParseFailure
from app.models import Block


class PptxParser:
    def parse(self, data: bytes, filename: str) -> tuple[str, list[Block]]:
        prs = Presentation(io.BytesIO(data))
        blocks: list[Block] = []
        title = ""
        for slide in prs.slides:
            for shape in slide.shapes:
                if not shape.has_text_frame:
                    continue
                text = shape.text_frame.text.strip()
                if not text:
                    continue
                if shape == slide.shapes.title:
                    blocks.append(Block(type="heading", text=text, level=1))
                    if not title:
                        title = text
                else:
                    blocks.append(Block(type="paragraph", text=text))
        if not blocks:
            raise ParseFailure("pptx: no text content found")
        return title or filename.rsplit(".", 1)[0], blocks
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd parser && python -m pytest tests/test_pptx_parser.py -v`
Expected: 1 passed

- [ ] **Step 5: Commit**

```bash
git add parser/
git commit -m "feat(parser): add pptx parser"
```

---

### Task 7: registry + fetcher + /parse API

**Files:**
- Create: `parser/app/registry.py`
- Create: `parser/app/fetcher.py`
- Create: `parser/app/main.py`
- Test: `parser/tests/test_registry.py`, `parser/tests/test_api.py`

**Interfaces:**
- Consumes: Task 1-6 全部 Parser 与 `ParseRequest/ParseResponse/ParseError/FetchFailure/ParseFailure`
- Produces(**P2 直接依赖,与 Roadmap 契约一致**):
  - `GET /health` → `{"status": "ok"}`
  - `POST /parse`,body `ParseRequest`;200→`ParseResponse`;内容不可解析→422 `{"error": str}`;拉取失败→502 `{"error": str}`
  - file_type 集合:`pdf | docx | pptx | md | txt | html`

- [ ] **Step 1: 写失败测试**

`parser/tests/test_registry.py`:
```python
import pytest

from app.errors import ParseFailure
from app.registry import get_parser


def test_all_supported_types_registered():
    for ft in ("pdf", "docx", "pptx", "md", "txt", "html"):
        assert get_parser(ft) is not None


def test_unsupported_type_raises():
    with pytest.raises(ParseFailure, match="unsupported"):
        get_parser("exe")
```

`parser/tests/test_api.py`:
```python
import pytest
from fastapi.testclient import TestClient

from app import main
from app.errors import FetchFailure

client = TestClient(main.app)


def test_health():
    assert client.get("/health").json() == {"status": "ok"}


def test_parse_txt_via_api(sample_txt, monkeypatch):
    async def fake_fetch(url: str) -> bytes:
        return sample_txt

    monkeypatch.setattr(main, "fetch_file", fake_fetch)
    resp = client.post("/parse", json={"file_url": "http://x/f.txt", "file_type": "txt"})
    assert resp.status_code == 200
    body = resp.json()
    assert body["title"] == "f"
    assert body["blocks"][0]["type"] == "paragraph"


def test_parse_unsupported_type(monkeypatch):
    async def fake_fetch(url: str) -> bytes:
        return b"x"

    monkeypatch.setattr(main, "fetch_file", fake_fetch)
    resp = client.post("/parse", json={"file_url": "http://x/f.exe", "file_type": "exe"})
    assert resp.status_code == 422
    assert "error" in resp.json()


def test_parse_fetch_failure_returns_502(monkeypatch):
    async def fake_fetch(url: str) -> bytes:
        raise FetchFailure("connection refused")

    monkeypatch.setattr(main, "fetch_file", fake_fetch)
    resp = client.post("/parse", json={"file_url": "http://x/f.txt", "file_type": "txt"})
    assert resp.status_code == 502
    assert "connection refused" in resp.json()["error"]
```

`parser/tests/conftest.py` 追加:
```python
@pytest.fixture(scope="session")
def sample_txt() -> bytes:
    return "第一段。\n\n第二段。".encode("utf-8")
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd parser && python -m pytest tests/test_registry.py tests/test_api.py -v`
Expected: FAIL,`ModuleNotFoundError: No module named 'app.registry'`

- [ ] **Step 3: 实现**

`parser/app/registry.py`:
```python
from app.errors import ParseFailure
from app.parsers.base import Parser
from app.parsers.docx_parser import DocxParser
from app.parsers.html_parser import HtmlParser
from app.parsers.markdown_parser import MarkdownParser
from app.parsers.pdf_parser import PdfParser
from app.parsers.pptx_parser import PptxParser
from app.parsers.text_parser import TextParser

PARSERS: dict[str, Parser] = {
    "pdf": PdfParser(),
    "docx": DocxParser(),
    "pptx": PptxParser(),
    "md": MarkdownParser(),
    "txt": TextParser(),
    "html": HtmlParser(),
}


def get_parser(file_type: str) -> Parser:
    parser = PARSERS.get(file_type.lower())
    if parser is None:
        raise ParseFailure(f"unsupported file_type: {file_type}")
    return parser
```

`parser/app/fetcher.py`:
```python
import httpx

from app.errors import FetchFailure

_MAX_BYTES = 50 * 1024 * 1024  # 50MB


async def fetch_file(url: str) -> bytes:
    try:
        async with httpx.AsyncClient(timeout=30.0, follow_redirects=True) as client:
            async with client.stream("GET", url) as resp:
                if resp.status_code != 200:
                    raise FetchFailure(f"fetch {url}: status {resp.status_code}")
                chunks: list[bytes] = []
                size = 0
                async for chunk in resp.aiter_bytes(65536):
                    size += len(chunk)
                    if size > _MAX_BYTES:
                        raise FetchFailure(f"fetch {url}: file exceeds 50MB limit")
                    chunks.append(chunk)
                return b"".join(chunks)
    except httpx.HTTPError as exc:
        raise FetchFailure(f"fetch {url}: {exc}") from exc
```

`parser/app/main.py`:
```python
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

from app.errors import FetchFailure, ParseFailure
from app.fetcher import fetch_file
from app.models import ParseRequest, ParseResponse
from app.registry import get_parser

app = FastAPI(title="open-ima parser")


@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok"}


@app.exception_handler(ParseFailure)
def parse_failure_handler(_: Request, exc: ParseFailure) -> JSONResponse:
    return JSONResponse(status_code=422, content={"error": str(exc)})


@app.exception_handler(FetchFailure)
def fetch_failure_handler(_: Request, exc: FetchFailure) -> JSONResponse:
    return JSONResponse(status_code=502, content={"error": str(exc)})


@app.post("/parse", response_model=ParseResponse)
async def parse(req: ParseRequest) -> ParseResponse:
    parser = get_parser(req.file_type)
    data = await fetch_file(req.file_url)
    filename = req.file_url.rsplit("/", 1)[-1] or f"file.{req.file_type}"
    title, blocks = parser.parse(data, filename)
    return ParseResponse(title=title, blocks=blocks)
```

注意:`test_parse_txt_via_api` 期望 title 为 `"f"`(从 URL 末段取文件名),这是上面 `filename` 提取逻辑的直接结果,不要改动测试去迁就实现。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd parser && python -m pytest tests/ -v`
Expected: 全部 passed(含此前 Task 的用例,约 15 个)

- [ ] **Step 5: Commit**

```bash
git add parser/
git commit -m "feat(parser): add registry, fetcher and /parse API"
```

---

### Task 8: Dockerfile + 容器内验证

**Files:**
- Create: `parser/Dockerfile`
- Create: `parser/.dockerignore`

**Interfaces:**
- Produces: 镜像 `open-ima-parser`,容器内 8100 端口,P2 compose 直接引用

- [ ] **Step 1: 写 Dockerfile**

`parser/Dockerfile`:
```dockerfile
FROM python:3.11-slim

WORKDIR /srv
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY app/ ./app/

EXPOSE 8100
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8100"]
```

`parser/.dockerignore`:
```
.venv/
tests/
__pycache__/
*.pyc
```

- [ ] **Step 2: 构建并验证**

Run:
```bash
cd parser
docker build -t open-ima-parser .
docker run -d --rm --name ima-parser-test -p 8100:8100 open-ima-parser
curl -s http://localhost:8100/health
# 预期: {"status":"ok"}
# 容器内解析验证(用宿主 python 起一个静态文件服务提供样例文件):
echo -e "第一段。\n\n第二段。" > /tmp/sample.txt
(cd /tmp && python3 -m http.server 8999 &) 
curl -s -X POST http://localhost:8100/parse \
  -H 'Content-Type: application/json' \
  -d '{"file_url":"http://host.docker.internal:8999/sample.txt","file_type":"txt"}'
# 预期: 返回 title=sample, blocks 含两个 paragraph
docker stop ima-parser-test; pkill -f "http.server 8999" || true
```
Expected: health 与 parse 均按注释预期返回;`docker images open-ima-parser` 显示镜像 <200MB

- [ ] **Step 3: 跑全量测试 + 提交**

Run: `cd parser && python -m pytest tests/ -v`
Expected: 全部 passed

```bash
git add parser/Dockerfile parser/.dockerignore
git commit -m "feat(parser): add Dockerfile for parser sidecar"
```

---

## Self-Review 结论(编写时已完成)

- **Spec 覆盖**:spec §4 的接口、六类 parser、注册表、错误语义、轻量无模型、docling 可替换(Parser 协议)——均有对应 Task。网页 URL 抓取的 HTTP 获取在 P2(Go 侧)实现,不属于本计划,与 spec §3.1 一致。
- **接口一致性**:`Block/ParseRequest/ParseResponse/ParseFailure/FetchFailure/get_parser/fetch_file` 命名在全部 Task 间一致;API 契约与 Roadmap 契约逐字段一致。
- **Placeholder 扫描**:无 TBD/TODO;每个代码步骤含完整可运行代码。
