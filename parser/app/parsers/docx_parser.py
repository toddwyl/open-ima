import io

from docx import Document
from docx.table import Table
from docx.text.paragraph import Paragraph

from app.errors import ParseFailure
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
        if not blocks:
            raise ParseFailure("docx: no content")
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
