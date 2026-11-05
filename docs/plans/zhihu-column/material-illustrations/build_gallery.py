"""Build local reading previews and a portable manuscript bundle; no image editing."""
from pathlib import Path
from html import escape
import json
import re
import shutil
import zipfile
import markdown
from PIL import Image

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'publish-bundle'
ASSETS = OUT / 'assets'
ASSETS.mkdir(parents=True, exist_ok=True)
SERIES_INTRO = (ROOT / 'series-intro.md').read_text().strip()
ARTICLES = sorted(ROOT.glob('0[2-8]-*.md'))
# This directory contains generated copies; remove superseded article filenames after reordering.
active_names = {p.with_suffix(ext).name for p in ARTICLES for ext in ('.md', '.html')}
for generated in OUT.glob('0[2-8]-*'):
    if generated.is_file() and generated.suffix in ('.md', '.html') and generated.name not in active_names:
        generated.unlink()
PATTERN = re.compile(r'!\[([^\]]*)\]\(([^)]+)\)')
CSS = '''
:root{--ink:#252b36;--muted:#687080;--blue:#3158d8;--paper:#faf7f2}
*{box-sizing:border-box}html{scroll-behavior:smooth}body{margin:0;background:var(--paper);color:var(--ink);font-family:-apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif;line-height:1.9}
header,main{max-width:980px;margin:auto;padding:32px}header{padding-top:64px}header small{letter-spacing:.15em;color:var(--blue)}h1{font-size:clamp(29px,4vw,44px);line-height:1.4;letter-spacing:-.02em}h2{font-size:27px;margin-top:64px}h3{font-size:22px;margin-top:38px}a{color:var(--blue);text-underline-offset:4px}nav{display:flex;flex-wrap:wrap;gap:12px 24px}nav a{text-decoration:none}p{margin:1.2em 0}img{width:100%;height:auto;display:block;border-radius:16px}figure{margin:36px 0 56px}figcaption,.caption{font-size:15px;color:var(--muted);line-height:1.8}figure .type{font-size:13px;color:var(--blue)}article{padding-bottom:64px;border-bottom:1px solid #ddd7ce}.article-start{padding-top:36px}.article-index{color:var(--blue);font-weight:600}table{border-collapse:collapse;width:100%;font-size:15px;display:block;overflow:auto;margin:28px 0}th,td{padding:12px 16px;border:1px solid #dfdbd5;text-align:left;vertical-align:top}th{background:#e9edf9}td{background:#fffdfa}code{background:#eeeae4;padding:2px 5px;border-radius:4px;font-size:.88em}pre{overflow:auto;background:#eeeae4;padding:22px;border-radius:12px;line-height:1.6}pre code{padding:0}blockquote{margin:28px 0;padding:4px 24px;border-left:4px solid #76bfa8;background:#edf4ed}details{padding:16px 20px;background:#eeebe6;border-radius:12px;margin:24px 0}summary{cursor:pointer;color:var(--muted)}.lede{color:var(--muted)}.entry{padding:22px 0;border-bottom:1px solid #ddd7ce}.entry a{font-size:22px;font-weight:600}.entry p{margin:8px 0}.toolbar{margin-bottom:32px;font-size:15px}@media(max-width:650px){header,main{padding:24px 18px}header{padding-top:40px}h2{font-size:24px}h3{font-size:21px}table{font-size:14px}th,td{min-width:120px}figure{margin:24px 0 36px}}@media print{nav,.toolbar{display:none}body{background:white}article{break-before:page}img{max-height:70vh;object-fit:contain}}
'''

def page(title, body, subtitle=''):
    return ('<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">'
            '<meta name="viewport" content="width=device-width,initial-scale=1">'
            f'<title>{escape(title)}</title><style>{CSS}</style></head><body>'
            f'<header><small>AGENT 协作手记</small><h1>{escape(title)}</h1>'
            f'<p class="lede">{subtitle}</p></header><main>{body}</main></body></html>')

def render(text):
    result = markdown.markdown(text, extensions=['tables', 'fenced_code', 'sane_lists'])
    result = re.sub(r'<pre><code class="language-mermaid">(.*?)</code></pre>',
                    r'<details><summary>补充架构图 · Mermaid 源码</summary><pre><code>\1</code></pre></details>',
                    result, flags=re.S)
    result = re.sub(r'<p>(图 \d[^<]*(?:<[^/][^>]*>.*?</[^>]+>[^<]*)*)</p>',
                    r'<p class="caption">\1</p>', result)
    return result

gallery = []
combined = []
index = []
stats = []
for p in ARTICLES:
    text = p.read_text()
    assert text.split('\n\n', 2)[1] == SERIES_INTRO, f'标准开篇引文不一致：{p.name}'
    title = text.splitlines()[0].removeprefix('# ')
    number = int(p.name[:2])
    section_id = f'article-{number}'
    gallery.append(f'<article id="{section_id}"><p class="article-index">第 {number} 篇</p><h2>{escape(title)}</h2>')
    matches = list(PATTERN.finditer(text))
    for j, m in enumerate(matches, 1):
        alt, relative = m.groups()
        source = (ROOT / relative).resolve()
        assert source.exists(), source
        with Image.open(source) as image:
            image.verify()
        target = ASSETS / source.name
        shutil.copy2(source, target)
        tail = text[m.end():].lstrip().split('\n\n', 1)[0]
        kind = '材质插画 / 数据对照' if 'material-illustrations/' in relative else ('实际页面留存' if 'screenshots/' in relative else '论文原图')
        caption = markdown.markdown(tail) if tail.startswith('图 ') else ''
        gallery.append(f'<figure><p class="type">{escape(kind)} · {j}/{len(matches)}</p>'
                       f'<a href="{escape(relative, quote=True)}"><img loading="lazy" src="{escape(relative, quote=True)}" alt="{escape(alt, quote=True)}"></a>'
                       f'<figcaption>{caption}</figcaption></figure>')
    gallery.append('</article>')
    portable = PATTERN.sub(lambda m: f'![{m.group(1)}](assets/{Path(m.group(2)).name})', text)
    (OUT / p.name).write_text(portable)
    html_name = p.with_suffix('.html').name
    toolbar = '<p class="toolbar"><a href="index.html">← 返回目录</a> · <a href="all-articles.html">连续阅读七篇</a></p>'
    article_body = '\n'.join(portable.splitlines()[1:])
    rendered = render(article_body)
    (OUT / html_name).write_text(page(title, toolbar + rendered, f'第 {number} 篇 · {len(matches)} 张配图'))
    combined.append(f'<article class="article-start" id="{section_id}"><p class="article-index">第 {number} 篇</p><h1>{escape(title)}</h1>{rendered}</article>')
    index.append(f'<section class="entry"><a href="{html_name}">第 {number} 篇 · {escape(title)}</a><p>{len(matches)} 张配图 · <a href="{p.name}">Markdown 源稿</a></p></section>')
    stats.append({'file': p.name, 'images': len(matches), 'characters': len(text), 'new_illustrations': sum('material-illustrations/' in m.group(2) for m in matches)})

nav = '<nav>' + ''.join(f'<a href="#article-{n}">第 {n} 篇</a>' for n in range(2, 9)) + '</nav>'
(ROOT / 'figure-gallery.html').write_text(page('七篇配图审阅', nav + ''.join(gallery),
    '26 张材质插画与数据图表 + 2 张真实页面 + 3 张论文原图。点击图片查看原尺寸。<a href="publish-bundle/index.html">阅读完整稿件 →</a>'))
(OUT / 'all-articles.html').write_text(page('七篇完整稿件', nav + ''.join(combined), '身份与上下文、执行分工、任务、工作流、计划、记忆与开发实践 · 31 张配图'))
(OUT / 'index.html').write_text(page('从设计决策到开发反思', ''.join(index),
    '七篇专栏稿件 · 26 张统一风格插画与数据图表 · 5 张原始截图及论文图片<br><a href="all-articles.html">连续阅读全部稿件 →</a>'))
(OUT / 'README.md').write_text('''# Agent 协作手记 · 七篇图文稿件

打开 `index.html` 按篇阅读，或打开 `all-articles.html` 连续阅读。所有正文图片均已放入 `assets/`，离线可读。Markdown 为可编辑源稿，上传知乎时使用相应 PNG。

七篇编号为 2—8；第 1 篇开篇已发布。文章包含 26 张材质插画、2 张留存页面截图、3 张论文原图，共 31 张。插画不作为真实产品界面或原始事件记录。

阅读顺序为：第 2 篇身份与上下文，第 3 篇 Supervisor–Executor，第 4 篇 Task，第 5 篇工作流，第 6 篇 Plan/DAG，第 7 篇团队记忆，第 8 篇开发实践。先讨论 Agent 本身，再展开任务与团队协作。图片文件名保留稳定素材编号，不代表当前篇次。

HTML 预览将 3 段补充 Mermaid 源码收起，可展开复制；主要机制已配独立 PNG。源码链接仍需联网。论文结果是工作稿中的探索性结果，来源与边界见各篇正文及图注。

配图参考 guizang-material-illustration 技能，并按作者要求使用 Q 版人形机器人及丰富协调的配色。正文是本地稿件，本轮未执行知乎发布。
''')
(ROOT / 'material-illustrations/validation.json').write_text(json.dumps({'articles':stats,'total_images':sum(s['images'] for s in stats),'new_illustrations':sum(s['new_illustrations'] for s in stats),'portable_asset_count':len(list(ASSETS.glob('*.png')))},ensure_ascii=False,indent=2))
with zipfile.ZipFile(ROOT / 'zhihu-column-illustrated.zip', 'w', zipfile.ZIP_DEFLATED) as archive:
    for p in sorted(OUT.rglob('*')):
        if p.is_file():
            archive.write(p, p.relative_to(ROOT))
print(json.dumps(stats,ensure_ascii=False,indent=2))
print('Built gallery, seven HTML/Markdown articles, continuous reader and portable ZIP.')
