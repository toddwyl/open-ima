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
