from pydantic import BaseModel


class ParseRequest(BaseModel):
    file_url: str
    file_type: str


class Block(BaseModel):
    type: str  # heading | paragraph | list | table
    text: str
    level: int = 0  # heading 层级 1-6,其他类型恒为 0


class ParseResponse(BaseModel):
    title: str
    blocks: list[Block]


class ParseError(BaseModel):
    error: str
