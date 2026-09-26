import pytest

from app.errors import ParseFailure
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


def test_text_parser_empty_raises():
    with pytest.raises(ParseFailure):
        TextParser().parse("  \n\n  ".encode(), "empty.txt")
