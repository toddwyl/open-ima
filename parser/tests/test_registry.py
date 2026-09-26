import pytest

from app.errors import ParseFailure
from app.registry import get_parser


def test_all_supported_types_registered():
    for ft in ("pdf", "docx", "pptx", "md", "txt", "html"):
        assert get_parser(ft) is not None


def test_unsupported_type_raises():
    with pytest.raises(ParseFailure, match="unsupported"):
        get_parser("exe")
