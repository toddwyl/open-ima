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
