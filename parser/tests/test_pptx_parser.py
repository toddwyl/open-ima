import pytest

from app.errors import ParseFailure
from app.parsers.pptx_parser import PptxParser


def test_pptx_slides_to_blocks(sample_pptx):
    title, blocks = PptxParser().parse(sample_pptx, "deck.pptx")
    assert title == "PPTX 演示标题"
    headings = [b for b in blocks if b.type == "heading"]
    assert [h.text for h in headings] == ["PPTX 演示标题", "第二页标题"]
    paras = [b.text for b in blocks if b.type == "paragraph"]
    assert any("副标题内容" in t for t in paras)
    assert any("第二页正文要点" in t for t in paras)


def test_pptx_corrupt_raises():
    with pytest.raises(ParseFailure, match="corrupted"):
        PptxParser().parse(b"this is not a pptx file", "corrupt.pptx")
