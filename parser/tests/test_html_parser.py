from app.parsers.html_parser import HtmlParser

HTML = """<!DOCTYPE html>
<html><head><title>测试页面标题</title>
<style>body{color:red}</style></head>
<body>
<nav>导航链接,不应出现</nav>
<article>
<h1>文章主标题</h1>
<p>第一段正文,长度要足够长以通过 readability 的正文判定,所以需要多写一些内容才行。这里继续补充更多文字,让这一段的文本密度更高,从而帮助 readability 正确识别正文区域,而不是把它误判为导航或页脚之类的杂讯内容。</p>
<h2>小节标题</h2>
<p>第二段正文,同样写得足够长,保证 readability-lxml 把它当成真正的正文区域而不是杂讯。再补充一些说明性文字,使正文的总字符数达到 readability 的判定阈值以上,确保整篇文章连同后续的列表一起被保留下来。</p>
<ul><li>列表项甲,这是一个内容足够长的列表项,用来保证 readability 不会因为文本过短而把它从正文中剔除掉。</li><li>列表项乙,同样补充了足够的说明文字,使整个无序列表的总文本长度超过 readability 的最小阈值要求。</li></ul>
</article>
<footer>页脚信息,不应出现</footer>
<script>console.log("x")</script>
</body></html>
"""


def test_html_extracts_article_blocks():
    title, blocks = HtmlParser().parse(HTML.encode(), "page.html")
    assert title  # readability 提取到非空标题
    texts = [b.text for b in blocks]
    assert any("文章主标题" in t for t in texts)
    assert any("第一段正文" in t for t in texts)
    assert any("列表项甲" in t for t in texts)
    assert not any("导航链接" in t for t in texts)
    assert not any("页脚信息" in t for t in texts)


def test_html_heading_levels():
    _, blocks = HtmlParser().parse(HTML.encode(), "page.html")
    headings = {b.text: b.level for b in blocks if b.type == "heading"}
    assert headings.get("文章主标题") == 1
    assert headings.get("小节标题") == 2
