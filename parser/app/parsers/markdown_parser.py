from markdown_it import MarkdownIt

from app.errors import ParseFailure
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
        if not blocks:
            raise ParseFailure("markdown: no content")
        return title or filename.rsplit(".", 1)[0], blocks
