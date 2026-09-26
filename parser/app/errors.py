class ParseFailure(Exception):
    """文件内容不可解析(加密/损坏/无文本层)。HTTP 422,不可重试。"""


class FetchFailure(Exception):
    """按 URL 拉取文件失败(网络/超时/超限)。HTTP 502,可重试。"""
