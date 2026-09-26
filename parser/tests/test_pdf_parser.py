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
