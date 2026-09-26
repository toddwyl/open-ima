from typing import Protocol

from app.models import Block


class Parser(Protocol):
    """结构化解析器协议:字节流 -> (标题, block 序列)。

    实现约束:
    - 不抛未捕获异常;内容不可解析时抛 errors.ParseFailure
    - 不返回空 blocks(无内容应抛 ParseFailure)
    """

    def parse(self, data: bytes, filename: str) -> tuple[str, list[Block]]: ...
