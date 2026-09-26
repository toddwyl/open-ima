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
