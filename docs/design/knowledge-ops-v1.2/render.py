"""Render editable SVG design boards and PNG previews. No application code is changed."""
from pathlib import Path
from html import escape
import unicodedata
ROOT = Path(__file__).resolve().parents[3]
OUT = ROOT / 'docs/images'
BLUE='#245BDB'; NAVY='#172B46'; MUTED='#576A82'; BORDER='#DCE4EF'; BG='#F3F6FB'
TEAL='#137D6D'; AMBER='#946000'; RED='#B93848'; WHITE='#FFFFFF'
class Board:
 def __init__(self,w,h,title,desc):
  self.w,self.h=w,h; self.parts=[f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" viewBox="0 0 {w} {h}" role="img" aria-labelledby="title desc"><title id="title">{escape(title)}</title><desc id="desc">{escape(desc)}</desc><defs><marker id="arrow" markerWidth="9" markerHeight="9" refX="7" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8" fill="none" stroke="#7790B0" stroke-width="1.5"/></marker></defs><style>text{{font-family:"PingFang SC","Microsoft YaHei",sans-serif}}</style>'];self.rect(0,0,w,h,BG,r=0)
 def rect(self,x,y,w,h,fill=WHITE,stroke=None,r=16,dash=None):
  self.parts.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{r}" fill="{fill}"'+(f' stroke="{stroke}" stroke-width="1.5"' if stroke else '')+(f' stroke-dasharray="{dash}"' if dash else '')+'/>')
 def text(self,x,y,t,size=22,color=NAVY,weight=400,anchor='start'):
  self.parts.append(f'<text x="{x}" y="{y}" font-size="{size}" font-weight="{weight}" fill="{color}" text-anchor="{anchor}">{escape(str(t))}</text>')
 def lines(self,x,y,lines,size=21,color=MUTED,gap=34,weight=400):
  for i,t in enumerate(lines):self.text(x,y+i*gap,t,size,color,weight)
 def line(self,x1,y1,x2,y2,color=BORDER,width=1.5,dash=None,arrow=False):
  self.parts.append(f'<path d="M{x1},{y1} L{x2},{y2}" fill="none" stroke="{color}" stroke-width="{width}"'+(f' stroke-dasharray="{dash}"' if dash else '')+(' marker-end="url(#arrow)"' if arrow else '')+'/>')
 def path(self,d,arrow=True,dash=None):
  self.parts.append(f'<path d="{d}" fill="none" stroke="#7790B0" stroke-width="2"'+(' marker-end="url(#arrow)"' if arrow else '')+(f' stroke-dasharray="{dash}"' if dash else '')+'/>')
 def pill(self,x,y,t,color=BLUE,bg='#EAF0FF',w=None,size=18):
  w=w or sum(1 if unicodedata.east_asian_width(c) in 'WF' else .57 for c in t)*size+28
  self.rect(x,y,w,34,bg,r=8);self.text(x+w/2,y+24,t,size,color,500,'middle')
 def card(self,x,y,w,h,title,lines=(),accent=BLUE,fill=WHITE):
  self.rect(x,y,w,h,fill,BORDER);self.rect(x+20,y+24,5,25,accent,r=2);self.text(x+39,y+45,title,25,NAVY,600);self.lines(x+25,y+88,lines,size=18 if w<400 else 21)
 def heading(self,number,title,sub):
  self.pill(64,38,f'WEKNORA  /  DESIGN {number}',w=280,size=17)
  self.text(64,123,title,46,NAVY,650);self.text(66,167,sub,22,MUTED)
  self.pill(self.w-288,47,'v1.2 · 2026.09.28',w=224,size=18)
 def foot(self,t):self.text(64,self.h-30,t,18,MUTED)
 def save(self,name):
  path=OUT/(name+'.svg');path.write_text(''.join(self.parts)+'</svg>');return path

def shell(b,sub='覆盖与诊断总览'):
 b.rect(0,0,242,b.h,WHITE,r=0);b.line(242,0,242,b.h)
 b.rect(28,30,40,40,BLUE,r=11);b.text(48,59,'W',23,WHITE,650,'middle');b.text(80,59,'WeKnora',27,NAVY,650)
 b.text(30,124,'工作空间',17,MUTED);b.text(30,158,'供应链金融知识中心',20,NAVY,600)
 for i,label in enumerate(['对话','知识库','智能体','产物','知识运营','设置']):
  y=207+i*63
  if label=='知识运营':b.rect(16,y-33,210,49,'#EAF0FF',r=9)
  b.rect(30,y-20,17,17,'none',BLUE if label=='知识运营' else '#7E8DA2',r=4)
  b.text(64,y,label,21,BLUE if label=='知识运营' else MUTED,600 if label=='知识运营' else 400)
 b.text(29,b.h-72,'工作空间管理员',18,MUTED);b.text(29,b.h-39,'设计稿 v1.2',17,MUTED)
 b.text(280,51,'知识运营  /  '+sub,18,MUTED)
 b.pill(b.w-265,25,'示例数据 · 非真实统计',AMBER,'#FFF4DC',w=235,size=17)

def metric(b,x,y,w,label,value,desc,c):
 b.rect(x,y,w,153,WHITE,BORDER);b.text(x+22,y+34,label,20,MUTED);b.text(x+22,y+88,value,43,c,650);b.text(x+22,y+127,desc,17,MUTED)

def workbench_tabs(b,active):
 tabs=[(280,'覆盖与诊断总览',166),(515,'待复核',145),(704,'复核记录',110)]
 for x,t,w in tabs:
  selected=t.startswith(active);b.text(x,218,t,22,BLUE if selected else MUTED,600 if selected else 400)
  if selected:b.line(x,237,x+w,237,BLUE,3)
 b.line(280,239,1872,239)

def field(b,x,y,w,t):
 b.rect(x,y,w,44,WHITE,BORDER,r=7);b.text(x+14,y+29,t,18,MUTED);b.text(x+w-25,y+28,'⌄',17,MUTED)

def verdict_chip(b,x,y,t):
 colors={'支持':(TEAL,'#EAF6F2'),'部分支持':(AMBER,'#FFF4DC'),'与证据矛盾':(RED,'#FDEEF0'),'未找到支持':(RED,'#FDEEF0'),'无法验证':(MUTED,'#EDF1F6')}
 c,bg=colors[t];b.pill(x,y,t,c,bg,size=17)


def architecture():
 b=Board(1920,1470,'知识运营整体架构 v1.2','一期集中实现观测、回答验证、只读根因诊断、风险分流和重点复核，结论留存后结束；知识处置与在线防线为后续范围。')
 b.heading('01','先诊断，再分流，让人工聚焦关键问题','一期：观测 → 回答验证 → 根因诊断 → 风险分流 → 重点复核 / 抽检 → 结论留存')
 b.rect(64,211,332,947,WHITE,BORDER);b.text(90,257,'现有问答与数据',27,NAVY,600)
 for y,title,ls in [(293,'RAG 问答',['快速流水线 / RAG Agent','原文分块 · FAQ · 文档读取']), (456,'Wiki 问答',['搜索 / 页面阅读 / 来源回查','页面正文 · 摘要 · 关联信息']), (619,'Wiki + RAG',['按问题选择工具','记录当次实际使用的来源'])]:b.card(84,y,292,144,title,ls,fill='#F8FAFD')
 b.line(88,797,372,797);b.text(90,838,'启用后新问答',23,NAVY,600);b.lines(90,877,['运行开始时确定采集范围','不补采旧问答或停用期数据'],19,gap=33)
 b.rect(84,970,292,155,'#FFF8E8');b.text(104,1008,'事实边界',22,AMBER,600);b.lines(104,1046,['当时证据与事后补查分开','缺失与未知必须保留'],19,AMBER,gap=32)
 b.rect(452,211,1000,947,'#EBF1FF','#A8BEED');b.text(480,257,'集中模块  internal/knowledgeops',29,NAVY,650);b.pill(1236,232,'一期 · 默认关闭',w=190,size=17)
 b.card(480,288,944,129,'薄接入与独立证据档案',['运行身份 / 实际可读内容 / 来源版本 / 截断与完成状态'],BLUE)
 b.path('M396,537 L422,537 L422,353 L470,353')
 stages=[(450,'01  观测与回答验证',['检查覆盖与失败原因；按论断判断当时证据支持性'],BLUE),(607,'02  只读根因诊断',['链路排查 + 按需来源比较；保留支持、反对证据和未知项'],TEAL),(764,'03  风险分流与问题归组',['低风险自动记录 / 分层抽检 / 业务复核 / 待补证 / 技术排查'],BLUE)]
 for y,title,ls,c in stages:
  b.card(480,y,944,124,title,ls,c);b.line(952,y-31,952,y-9,'#7790B0',2,arrow=True)
 b.line(952,888,952,914,'#7790B0',2,arrow=True)
 b.rect(480,928,944,197,WHITE,BORDER);b.text(506,970,'独立保存结论与操作历史',26,NAVY,600);b.lines(506,1011,['运行 / 证据 / 验证 / 诊断 / 分流 / 人工复核 / 任务','回答与根因分别复核，自动记录不计作人工确认','权限继承来源；按版本追加，重跑不覆盖历史'],21,gap=35)
 b.rect(1510,211,346,947,WHITE,BORDER);b.text(1536,257,'运营工作台',28,NAVY,600)
 for y,title,ls,c in [(293,'覆盖与诊断总览',['证据覆盖 · 根因分布','分流量 · 人工负担 · 漏报'],BLUE),(488,'待复核',['先看诊断包与待裁决点','同源同版本问题归组','高风险保留人工复核'],AMBER),(706,'复核记录',['回答与根因分别确认','人工意见及机器版本可追溯','自动记录单独展示'],TEAL)]:b.card(1532,y,302,175,title,ls,c,'#F8FAFD')
 b.rect(1532,930,302,195,'#EAF6F2');b.text(1554,970,'本期止于结论留存',23,TEAL,600);b.lines(1554,1012,['允许写入运营记录','不修改原文 / Wiki / FAQ','确认诊断不触发修复'],19,TEAL,gap=33)
 b.line(1452,827,1500,827,'#7790B0',2,arrow=True)
 b.rect(64,1190,880,126,WHITE,BORDER,dash='7 5');b.text(89,1231,'后续 L3 · 经审批的知识处置',25,MUTED,600);b.text(89,1276,'主动检查 → 变更提案 → 审批 → 执行 → 原模式回归',21,MUTED)
 b.rect(974,1190,882,126,WHITE,BORDER,dash='7 5');b.text(999,1231,'远期 L4 · 在线防线',25,MUTED,600);b.text(999,1276,'完成业务校准后，另行设计门控、拒答与流式发布策略',21,MUTED)
 b.rect(64,1340,1792,65,NAVY);b.text(90,1381,'main 合并边界：业务集中 · 只读适配器 · 少量采集与注册接线 · 独立迁移 · 不复制问答引擎',23,WHITE,500)
 b.foot('v1.2 设计基线 · Langfuse 为可选辅助；当前一期不接通知识写入。')
 return b.save('knowledge-ops-architecture-v1.2')

def flow():
 b=Board(1920,1630,'一期观测诊断与分流流程 v1.2','仅采集启用后的新问答，先验证回答再做分层诊断，依据风险分流；补查有限、版本可追溯，高风险与未知不自动通过。')
 b.heading('02','一期闭环：机器先排查，人工做裁决','全量检查基础事实，异常、高风险与抽检案例按需深查；证据不足时保留未知。')
 b.card(64,210,565,149,'新运行身份与范围',['渠道 / 模式 / 配置快照 / 启用状态','只纳入启用后新发起的问答'],BLUE)
 b.card(678,210,565,149,'新增问答采集',['实际进入模型的证据与来源元数据','记录完成、取消、缺失和截断'],BLUE)
 b.card(1292,210,564,149,'事后只读补查',['原文 / Wiki / 修订 / 可选日志','独立时间与版本，不冒充当时证据'],TEAL)
 b.path('M346,359 L346,389 L656,389 L656,412');b.path('M961,359 L961,412')
 b.rect(64,426,1179,131,'#EAF0FF','#B9CCF3');b.text(90,468,'01  统一档案与证据覆盖',27,BLUE,600);b.lines(90,510,['完整 / 部分 / 缺失或未知 → 明确可验证范围','渠道 · 配置模式 · 实际执行与来源 · 正文快照 · 版本 · 使用位置'],20,gap=31)
 b.line(654,557,654,589,'#7790B0',2,arrow=True)
 b.card(64,603,1179,127,'02  异步回答验证',['逐论断 / JSON 字段：支持、部分支持、矛盾、未找到支持、无法验证'],BLUE)
 b.line(654,730,654,762,'#7790B0',2,arrow=True)
 b.rect(64,776,1179,169,WHITE,BORDER);b.text(90,817,'03  根因诊断',27,TEAL,600);b.lines(90,857,['基础排查：采集 / 权限 / 工具 / 上下文 / 生成；按需比较 Wiki 与原文','输出：观察事实 → 根因候选 → 支持与反对证据 → 未知项 → 待裁决点','同一来源版本与适用条件的问题可归组，每条回答仍保留自身判定'],21,gap=33)
 b.path('M1574,359 L1574,398 L1882,398 L1882,753 L1270,753 L1270,839 L1254,839');b.text(1410,417,'仅用于事后核验',19,TEAL)
 b.card(1292,460,564,270,'诊断执行边界',['只读工具允许清单，继承来源权限','补查次数 / 时限 / 模型费用有预算','版本缺失、失败、预算耗尽均保留未知','模型自报置信度不单独决定免人工'],AMBER,'#FFFAEF')
 b.rect(1292,776,564,169,'#EAF6F2',BORDER);b.text(1318,817,'较完整 ≠ 强行给定论',26,TEAL,600);b.lines(1318,859,['异常、高风险与抽检案例按需深查','未做来源核验时，明确显示未核验','疑似根因与已确认事实分开'],21,TEAL,gap=32)
 b.line(960,945,960,973,'#7790B0',2,arrow=True)
 b.rect(64,988,1792,75,NAVY);b.text(960,1034,'04  风险分流  ·  综合业务风险、证据完整性、可检查结果与校准规则',27,WHITE,600,'middle')
 routes=[(64,'低风险且符合通过规则',['自动记录机器结论','按模式 / 渠道 / 来源分层抽检'],TEAL,'#EFF9F5'),(520,'高风险 / 冲突 / 关键不确定',['诊断包 → 重点人工复核','AI 判为支持也不能跳过'],RED,'#FFF2F3'),(976,'证据不足 / 根因难区分',['有界补查 → 再诊断与分流','仍不足：待补证 / 保留未知'],AMBER,'#FFFAEF'),(1432,'采集 / 工具 / 任务故障',['技术排查 → 按版本重试','高风险业务同时保留复核'],MUTED,'#F8FAFD')]
 for x,title,ls,c,bg in routes:
  b.line(x+212,1063,x+212,1093,'#7790B0',2,arrow=True);b.card(x,1107,424,166,title,ls,c,bg)
 b.rect(64,1310,1792,111,WHITE,BORDER);b.text(90,1351,'05  结论留存与校准反馈',27,NAVY,600);b.text(90,1391,'回答判定与根因分别复核 · 自动记录与人工确认分开 · 新证据触发新版本 · 只保存运营记录',23,MUTED)
 for x in [276,732,1188,1644]:b.line(x,1273,x,1300,'#7790B0',2,arrow=True)
 b.rect(64,1455,1792,105,'#EAF0FF');b.text(90,1494,'采集从启用时开始，后台任务可恢复',24,BLUE,600);b.text(90,1534,'不适配或回填旧问答；仅恢复已采集运行的任务。新问答实际使用的会话上下文与知识修订仍可核验。',21,MUTED)
 b.foot('仅保存运营记录；不修改原文、Wiki、FAQ，不触发知识修复。图中路径均为设计方案。')
 return b.save('verification-system-design-v1.2')

def dashboard():
 b=Board(1920,1510,'覆盖与诊断总览 v1.2','示例1200条回答；证据完整840、部分240、缺失120。已验证完整证据760条的主分流合计760，自动记录480中抽检24。')
 shell(b);b.text(280,120,'覆盖与诊断总览',35,NAVY,650);b.text(280,157,'证据覆盖、诊断结论与人工负担，放在同一个时间窗口观察。',21,MUTED)
 field(b,1480,92,180,'诊断设置');b.rect(1676,92,196,47,BLUE,r=8);b.text(1774,123,'查看采集状态',19,WHITE,550,'middle')
 workbench_tabs(b,'覆盖')
 for x,w,t in [(280,390,'2026/09/01 — 2026/09/28'),(686,228,'渠道：全部'),(930,294,'问答类型：全部'),(1240,294,'智能体：全部')]:field(b,x,260,w,t)
 b.text(1554,289,'采集启用 09/01 · 上海时间',17,MUTED)
 for x,label,val,desc,c in [(280,'范围内回答','1,200','三类问答，包含证据缺失',BLUE),(682,'证据完整','840','占全部回答 70.0%',TEAL),(1084,'证据部分完整','240','占全部回答 20.0%',AMBER),(1486,'证据缺失 / 未知','120','占全部回答 10.0%',MUTED)]:metric(b,x,328,386,label,val,desc,c)
 b.rect(280,505,984,308,WHITE,BORDER);b.text(305,546,'三类问答的证据覆盖',25,NAVY,600)
 cols=[(305,'问答类型'),(638,'回答数'),(794,'完整'),(947,'部分'),(1093,'缺失')]
 b.rect(302,567,940,43,'#F6F8FC',r=6)
 for x,t in cols:b.text(x,596,t,18,MUTED)
 for i,row in enumerate([('RAG 问答',400,320,60,20),('Wiki 问答',300,180,90,30),('Wiki + RAG',500,340,90,70)]):
  y=650+i*57
  for (x,_),v in zip(cols,row):b.text(x,y,v,21,NAVY,500)
  if i<2:b.line(305,y+18,1240,y+18)
 b.rect(1288,505,584,308,WHITE,BORDER);b.text(1313,546,'回答支持性',25,NAVY,600);b.text(1313,580,'分母：已验证且证据完整的 760 条',19,MUTED)
 for i,(t,n,c) in enumerate([('支持',570,TEAL),('部分支持',114,AMBER),('与证据矛盾',38,RED),('未找到支持',38,MUTED)]):
  y=622+i*37;b.text(1313,y,t,19,MUTED);b.text(1839,y,n,22,c,600,'end')
 b.text(1313,788,'另有 60 条待验证 / 处理中，20 条失败',17,MUTED)
 b.rect(280,838,1592,246,WHITE,BORDER);b.text(305,879,'诊断后的主分流',25,NAVY,600);b.text(1846,878,'同一组 760 条已验证回答 · 按最新主分流计数',18,MUTED,400,'end')
 for i,(label,n,desc,c) in enumerate([('自动记录',480,'满足规则；其中抽检 24 条',TEAL),('业务复核',140,'高风险 / 来源冲突 / 关键存疑',RED),('待补证',90,'明确缺口，有界补查后再分流',AMBER),('技术排查',50,'来源读取 / 诊断任务故障',MUTED)]):
  x=305+i*387;b.text(x,925,label,21,MUTED);b.text(x,976,n,37,c,650);b.text(x,1016,desc,18,MUTED)
  if i<3:b.line(x+356,906,x+356,1030)
 b.text(305,1060,'抽检为自动记录子集，不重复计入主分流总量；技术故障涉及高风险时，另保留业务复核待办。',18,MUTED)
 b.rect(280,1110,782,306,WHITE,BORDER);b.text(305,1152,'根因与来源核验',25,NAVY,600)
 b.pill(305,1174,'按需深查 240 / 760 条',w=302,size=18);b.pill(625,1174,'问题组 32 个',AMBER,'#FFF4DC',w=191,size=18)
 b.lines(305,1242,['常见候选：条件遗漏 / Wiki 更新滞后 / 来源适用性不清','来源核验：一致 150 · 不一致 50 · 未解决 40','其余 520 条未做来源深查，不标记为知识正确'],20,MUTED,gap=36)
 b.text(305,1383,'问题组数、潜在影响回答数与已确认错误数分别计算。',18,MUTED)
 b.rect(1086,1110,786,306,WHITE,BORDER);b.text(1111,1152,'人工负担与抽检质量',25,NAVY,600)
 for i,(label,val) in enumerate([('当前待办','业务 36 · 抽检 12 · 补证 16'),('复核耗时中位数','3.2 分钟 / 次'),('抽检漏报','已复核 12 条 · 暂未发现漏报'),('诊断成本中位数','¥0.18 / 条')]):
  y=1207+i*42;b.text(1111,y,label,19,MUTED);b.text(1840,y,val,20,NAVY,500,'end')
 b.text(1111,1383,'示例分层样本；不直接外推全量准确率。',18,MUTED)
 b.text(280,1470,'来源核验与回答支持性分开；一期只留存运营结论，人工确认不触发知识变更。',20,MUTED)
 return b.save('verification-page-ui-v1.2')

def detail():
 b=Board(1920,1600,'诊断与复核详情 v1.2','混合配置实际只读Wiki。机器已完成诊断：回答有Wiki支持，但补查原文出现年限差异，疑似更新滞后，历史生效范围缺证；人工分别复核回答与根因。')
 shell(b,'待复核 / 诊断详情');b.text(280,116,'诊断与复核详情',35,NAVY,650);b.text(280,154,'QA-DEMO-042  ·  API  ·  企业融资准入审核  ·  问题组 G-012',21,MUTED)
 for x,t,w,c,bg in [(280,'配置：Wiki + RAG',230,BLUE,'#EAF0FF'),(526,'实际证据：仅 Wiki',244,BLUE,'#EAF0FF'),(786,'采集完整',142,TEAL,'#EAF6F2'),(944,'高风险 · 优先复核',252,RED,'#FDEEF0')]:b.pill(x,182,t,c,bg,w=w)
 b.rect(280,238,1592,94,'#FFF4DC','#EDD8A7');b.text(305,276,'诊断已完成 · 疑似 Wiki 更新滞后，历史适用性仍缺证',26,AMBER,600);b.text(305,313,'待裁决：D1 的“一年”要求在问答发生时是否已对该产品和主体生效？',21,NAVY)
 # Upper case and diagnosis panels.
 b.rect(280,356,506,385,WHITE,BORDER);b.text(305,398,'问题与回答时依据',26,NAVY,600)
 b.lines(305,441,['问：企业融资准入要求成立至少几年？','答：企业成立须满两年。'],21,NAVY,gap=36)
 b.line(305,502,761,502);b.text(305,539,'论断 C1 · $.conclusion',19,MUTED)
 b.rect(305,560,456,88,'#EFF9F5');b.lines(325,595,['回答支持性：支持','与当时实际读取的 W1 一致'],21,TEAL,gap=32)
 b.lines(305,686,['09:10 读取 Wiki v12，未截断、已送入模型','当次没有读取原文；不据此判定工具使用错误'],17,MUTED,gap=29)
 b.rect(810,356,1062,385,WHITE,BORDER);b.text(836,398,'机器根因诊断  D-042 / v1',26,NAVY,600);b.pill(1623,371,'09:16 已完成',TEAL,'#EAF6F2',w=224,size=18)
 b.text(836,444,'候选 1 · Wiki 更新滞后（疑似）',24,AMBER,600)
 b.lines(836,485,['支持依据：W1 写“两年”，关联原文 D1 r7 写“一年”；产品与主体字段一致。','限制 / 反对线索：问答时适用的原文修订未取到；可能存在历史版本或特殊条款。','已排查：论断与 W1 一致；未发现本次生成篡改年限，采集未见截断。'],20,MUTED,gap=37)
 b.line(836,584,1847,584);b.text(836,622,'仍需补证',21,NAVY,600)
 b.lines(836,659,['D1 的生效日期、历史修订与特殊条款；当前差异不足以断言当时 Wiki 错误。','分流依据：准入年限属于高风险 + 来源差异 + 关键适用性不清。'],20,MUTED,gap=35)
 # Original evidence vs post-hoc material, with explicit timelines.
 b.rect(280,765,1012,474,WHITE,BORDER);b.text(305,808,'可追溯证据链',26,NAVY,600)
 b.pill(305,832,'W1 · 回答时实际证据',w=277,size=18);b.text(305,902,'企业融资准入 Wiki · v12 · 09:10',23,NAVY,600)
 b.rect(305,925,962,61,'#EFF9F5',r=9);b.text(325,965,'“申请企业应依法设立，且成立时间须满两年。”',24,TEAL)
 b.line(305,1010,1267,1010)
 b.pill(305,1028,'D1 · 诊断器事后补查',AMBER,'#FFF4DC',w=277,size=18);b.text(305,1097,'融资准入制度 · r7 · 09:13 只读获取',23,NAVY,600)
 b.rect(305,1120,962,61,'#FFFAEF',r=9);b.text(325,1160,'“申请企业成立时间须满一年。”',24,AMBER)
 b.text(305,1213,'D1 不计入模型当时证据；历史生效范围未确认，诊断明确保留未知。',19,MUTED)
 # Independent verdict controls.
 b.rect(1316,765,556,474,WHITE,BORDER);b.text(1341,808,'人工分别复核',26,NAVY,600)
 b.text(1341,849,'回答判定',19,MUTED);field(b,1341,865,506,'确认：得到当时证据支持')
 b.text(1341,954,'根因结论',19,MUTED);field(b,1341,970,506,'待补证：尚不能确认更新滞后')
 b.text(1341,1059,'复核理由 / 所需材料',19,MUTED);b.rect(1341,1076,506,126,'#FCFDFE',BORDER,r=9)
 b.lines(1359,1110,['W1 足以支持回答论断；','请补充 D1 的历史修订与生效依据，','再确认是否属于 Wiki 更新滞后。'],20,MUTED,gap=33)
 b.rect(280,1263,1592,113,'#EAF0FF',BORDER);b.text(305,1305,'问题组 G-012 · 关联 18 条回答',24,BLUE,600);b.text(305,1344,'共享来源与诊断线索；本次仅复核 QA-DEMO-042 的 C1 和 D-042/v1，不自动确认其他回答。',21,MUTED)
 b.rect(280,1400,1592,88,WHITE,BORDER);b.text(305,1453,'复核人：业务专家  ·  保存为追加记录',19,MUTED)
 b.rect(1290,1418,185,48,WHITE,BORDER,r=8);b.text(1382,1450,'查看诊断轨迹',19,BLUE,500,'middle')
 b.rect(1491,1418,354,48,BLUE,r=8);b.text(1668,1450,'保存两项复核结论',20,WHITE,550,'middle')
 b.text(280,1545,'当前选择尚未提交。保存只留存复核意见，不修改知识；补证后生成新诊断版本。',20,MUTED)
 return b.save('verification-review-detail-v1.2')

def review_queue():
 b=Board(1920,1470,'待复核与技术待办 v1.2','按风险和业务影响展示已诊断案例及问题组，包含业务复核、抽检、待补证和技术排查；每条回答独立确认。')
 shell(b,'待复核');b.text(280,120,'待复核',35,NAVY,650);b.text(280,157,'先看机器排查结果，再处理需要裁决的事项。重复问题集中查看，结论按范围保存。',21,MUTED)
 b.rect(1664,92,208,47,BLUE,r=8);b.text(1768,123,'开始优先复核',19,WHITE,550,'middle');workbench_tabs(b,'待复核')
 for x,label,val,desc,c in [(280,'业务待复核','36','高风险、来源冲突或关键存疑',RED),(682,'抽检待办','12','从自动记录中分层抽样',TEAL),(1084,'待补证','16','已诊断出具体材料缺口',AMBER),(1486,'技术待办','18','其中 4 条另有业务复核待办',MUTED)]:metric(b,x,263,386,label,val,desc,c)
 b.rect(280,439,1592,65,'#EAF0FF',BORDER,r=10);b.text(305,479,'按队列计待办，可跨队列关联；自动记录不等于人工确认，高风险回答即使被证据支持也需复核。',20,BLUE,500)
 for x,w,t in [(280,310,'队列：业务 / 抽检 / 补证'),(606,264,'问答类型：全部'),(886,270,'风险等级：全部'),(1172,294,'根因类别：全部'),(1482,390,'视图：问题组与独立案例')]:field(b,x,528,w,t)
 b.rect(280,595,1592,681,WHITE,BORDER);b.text(305,636,'诊断后的复核任务',25,NAVY,600);b.text(1847,635,'按风险 → 业务影响 → 入队时间排序',18,MUTED,400,'end')
 b.rect(302,658,1548,46,'#F6F8FC',r=6)
 headers=[(305,'风险 / 队列'),(467,'问题 / 来源范围'),(909,'诊断摘要 / 待裁决点'),(1325,'证据 / 关联回答'),(1570,'模式 / 实际来源'),(1760,'操作')]
 for x,t in headers:b.text(x,688,t,17,MUTED)
 rows=[
 ('高','业务复核','企业成立至少几年？','G-012 · 准入 Wiki v12 / D1 r7','疑似 Wiki 更新滞后','待裁决：原文历史生效与特殊条款','当时完整 / 补查缺证','关联 18 条 · 未批量确认','Wiki + RAG','API · 仅 Wiki','查看诊断'),
 ('高','业务复核','融资期限能否超过合同有效期？','G-019 · 融资制度 r8','回答遗漏期限限制条件','待裁决：例外授权是否适用','完整 / 对比已完成','关联 9 条 · 未批量确认','Wiki + RAG','API · Wiki + 原文','查看诊断'),
 ('高','待补证','提前还款是否支付违约金？','QA-DEMO-039 · 合同审核助手','文档读取正文未保存','待补证：当时有效的合同条款','部分 / 技术单 T-008','1 条 · 技术侧同步排查','RAG 问答','API · 原文读取','查看缺口'),
 ('中','业务复核','展期需要哪些审批条件？','G-023 · 展期 Wiki v6','Wiki 归纳疑似遗漏例外','待裁决：特批条款与普通流程边界','完整 / 对比已完成','关联 4 条 · 未批量确认','Wiki 问答','Web · Wiki','查看诊断'),
 ('低','抽检','供应商准入流程有哪些环节？','QA-DEMO-036 · 流程 Wiki v9','抽检深查未发现来源差异','核验机器漏报；保留抽样层与概率','完整 / 抽检深查完成','1 条 · 来源分层抽中','Wiki 问答','Web · Wiki','进入抽检'),
 ('低','待补证','哪些附件需要加盖公章？','QA-DEMO-031 · 制度问答助手','来源适用对象不明确','待补证：产品范围，暂不判通过','部分 / 补查预算已到','1 条 · 保留未解决','RAG 问答','Web · 原文','查看缺口')]
 for i,(risk,queue,q,group,cause,decision,evidence,count,mode,source,action) in enumerate(rows):
  top=716+i*81
  if i==0:b.rect(302,top-4,1548,80,'#F1F5FF',r=0)
  c=RED if risk=='高' else AMBER if risk=='中' else TEAL;bg='#FDEEF0' if risk=='高' else '#FFF4DC' if risk=='中' else '#EAF6F2'
  b.pill(305,top+1,risk,c,bg,w=48,size=17);b.text(305,top+58,queue,17,MUTED)
  b.text(467,top+27,q,19,NAVY,500);b.text(467,top+58,group,16,MUTED)
  b.text(909,top+27,cause,19,NAVY,500);b.text(909,top+58,decision,16,MUTED)
  b.text(1325,top+27,evidence,17,AMBER if '缺' in evidence or '部分' in evidence else TEAL);b.text(1325,top+58,count,16,MUTED)
  b.text(1570,top+27,mode,18,NAVY);b.text(1570,top+58,source,16,MUTED);b.text(1760,top+27,action,18,BLUE,500)
  if i<5:b.line(305,top+77,1847,top+77)
 b.line(305,1211,1847,1211);b.text(305,1252,'当前展示 6 个问题组 / 独立案例；展开后按回答与诊断版本复核。',18,MUTED)
 b.rect(280,1300,1592,104,WHITE,BORDER);b.text(305,1341,'技术队列独立处理，业务风险持续可见',24,NAVY,600);b.text(305,1378,'采集、工具与任务故障由技术角色排查；高风险案例保留业务待办。补证与重试生成新版本。',20,MUTED)
 b.text(280,1440,'同一租户、来源版本与适用条件归组；共享诊断不自动覆盖成员回答结论。',19,MUTED)
 return b.save('verification-review-queue-v1.2')

def review_records():
 b=Board(1920,1470,'复核记录与双结论历史 v1.2','追加保存人工对回答或根因的复核动作，关联机器诊断版本；机器自动记录独立展示，不能冒充人工确认。')
 shell(b,'复核记录');b.text(280,120,'复核记录',35,NAVY,650);b.text(280,157,'回答判定与根因结论分别追溯；每次人工操作保留对应机器版本、证据与理由。',21,MUTED);workbench_tabs(b,'复核记录')
 for x,label,val,desc,c in [(280,'人工复核操作','86','按操作次数，非独立回答数',BLUE),(682,'回答判定复核','58','针对回答证据支持性',TEAL),(1084,'根因结论复核','28','针对诊断原因与来源适用性',AMBER),(1486,'机器自动记录','480','单独查看，不计入人工 86 次',MUTED)]:metric(b,x,263,386,label,val,desc,c)
 b.rect(280,439,1592,65,'#EAF0FF',BORDER,r=10);b.text(305,479,'当前：人工复核历史。自动记录保留机器来源；再次诊断或人工纠正不覆盖原记录。',21,BLUE,500);b.text(1710,479,'查看机器记录 →',18,BLUE,600)
 for x,w,t in [(280,360,'2026/09/01 — 2026/09/28'),(656,260,'复核对象：全部'),(932,260,'复核动作：全部'),(1208,250,'复核人：全部'),(1474,398,'搜索案例 / 问题组 / 理由')]:field(b,x,528,w,t)
 b.rect(280,595,1592,681,WHITE,BORDER);b.text(305,636,'人工复核历史',25,NAVY,600);b.text(1847,635,'按操作时间倒序 · 两项结论分别保存',18,MUTED,400,'end')
 b.rect(302,658,1548,46,'#F6F8FC',r=6)
 headers=[(305,'案例 / 关联版本'),(680,'复核对象'),(844,'机器原结论'),(1138,'人工结论 / 动作'),(1450,'复核人 / 时间'),(1760,'操作')]
 for x,t in headers:b.text(x,688,t,17,MUTED)
 rows=[
 ('准入年限要求适用于该企业吗？','QA-DEMO-041 · V2 / D2 · G-010','根因结论','来源适用性不清','诊断：未解决','特殊条款不适用','纠正根因 · 已补足适用依据','张审核','09-28 10:06'),
 ('准入年限要求适用于该企业吗？','QA-DEMO-041 · V2 / D2 · G-010','回答判定','与证据矛盾','依据：E-041/v2','与证据矛盾','确认回答判定','张审核','09-28 10:05'),
 ('合同签署必须提供授权书吗？','QA-DEMO-037 · V2 / D2 · 第 2 次','回答判定','未找到支持','依据：E-037/v2','部分支持','纠正判定 · 保留前次结论','李专家','09-28 09:48'),
 ('企业成立年限要求是什么？','QA-DEMO-029 · V1 / D1 · G-012','根因结论','疑似 Wiki 更新滞后','缺口：历史版本与生效依据','仍待补证','要求补证 · 不强行确认根因','张审核','09-28 09:13'),
 ('企业成立年限要求是什么？','QA-DEMO-029 · V1 / D1 · G-012','回答判定','支持','依据：当时 Wiki 快照','支持','确认回答 · 根因仍未解决','张审核','09-28 09:12'),
 ('供应商准入流程如何申请？','QA-DEMO-021 · V1 / D1 · 抽检','回答判定','支持','依据：E-021/v1','支持','抽检确认 · 来源层 S-Wiki-3','王审核','09-27 17:35')]
 for i,(q,case,target,ai,scope,human,action,reviewer,time) in enumerate(rows):
  top=716+i*81
  if target=='根因结论':b.rect(302,top-4,1548,80,'#FFFCF3',r=0)
  b.text(305,top+27,q,19,NAVY,500);b.text(305,top+58,case,16,MUTED)
  b.pill(680,top+6,target,AMBER if target=='根因结论' else BLUE,'#FFF4DC' if target=='根因结论' else '#EAF0FF',w=138,size=17)
  b.text(844,top+27,ai,20,NAVY,500);b.text(844,top+58,scope,16,MUTED)
  b.text(1138,top+27,human,20,AMBER if '补证' in human else RED if human=='与证据矛盾' else TEAL,600);b.text(1138,top+58,action,16,MUTED)
  b.text(1450,top+27,reviewer,19,NAVY);b.text(1450,top+58,time,17,MUTED);b.text(1760,top+27,'查看记录',18,BLUE,500)
  if i<5:b.line(305,top+77,1847,top+77)
 b.line(305,1211,1847,1211);b.text(305,1252,'共 86 次人工操作 · 当前展示 1–6 条 · 同一案例可有不同复核对象与多个版本',18,MUTED)
 b.rect(280,1300,1592,104,WHITE,BORDER);b.text(305,1341,'一条记录关联一项结论与对应版本',24,NAVY,600);b.text(305,1378,'查看原始判定、支持 / 反对证据、人工理由及前后版本；确认回答有依据，不代表根因已经确定。',20,MUTED)
 b.text(280,1440,'复核历史采用追加记录；当前不包含知识修改、提案执行或发布操作。',19,MUTED)
 return b.save('verification-review-records-v1.2')

if __name__=='__main__':
 OUT.mkdir(parents=True,exist_ok=True)
 for generate in [architecture,flow,dashboard,detail,review_queue,review_records]:
  print(generate().relative_to(ROOT))
