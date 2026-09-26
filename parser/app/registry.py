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
