import pytest

from app.errors import ParseFailure
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


def test_markdown_empty_raises():
    with pytest.raises(ParseFailure):
        MarkdownParser().parse("  \n\n  ".encode(), "empty.md")
