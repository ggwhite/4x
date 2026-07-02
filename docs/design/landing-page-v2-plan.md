# Landing Page v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce `docs/index-v2.html` — a standalone draft landing page that refreshes stale
stats, fixes known markup bugs, and adds three new full sections (Multi-repo & Worktree
Isolation, Quality Gates, Cost Visibility) plus an enhanced Self-Evolution section, without
touching the live `docs/index.html`.

**Architecture:** Single self-contained HTML file (Tailwind CDN + inline `<script>` i18n
dict), same pattern as v1. New sections reuse existing CSS classes (`.feature-card`,
`.code-block`, `.gradient-text`) and the existing i18n mechanism (`data-i18n` attributes +
`i18n[lang]` lookup table + `switchLang()`). No build step, no new dependencies.

**Tech Stack:** Static HTML/CSS/JS, Tailwind CDN, `tidy` for markup validation (already
installed at `/usr/bin/tidy`), `python3 -m http.server` for local smoke testing.

## Global Constraints

- v1 (`docs/index.html`) must not be modified — v2 is a fully independent file.
- v2 only actively translates `en` + `zh-TW`. Do not add `ja`/`ko`/`es`/`zh-CN` entries for
  new keys — `switchLang()` already leaves DOM text untouched (falls back to whatever was
  last rendered, i.e. English) when a key is missing from the target language object, so
  this does not break those languages.
- Visual style is unchanged: dark theme (`bg`/`surface`/`border`/`muted`/`accent`/`green`/
  `purple`/`orange` Tailwind color tokens), no new color tokens, no layout-skeleton changes.
- New sections follow the existing section wrapper pattern:
  `<section class="max-w-4xl mx-auto px-6 py-16">` with a centered `<h2 data-i18n="X.title">`
  and `<p class="text-muted text-center mb-8 max-w-2xl mx-auto" data-i18n="X.desc">`.
- i18n key namespacing follows the existing dot-convention (`why.*`, `how.*`, `evo.*`) — new
  namespaces are `multirepo.*`, `quality.*`, `costviz.*`.
- No screenshots exist yet for Multi-repo or Quality Gates — those two sections use
  `.code-block` JSON examples (matching the existing Config Examples section style), not
  images. Cost Visibility reuses the existing `docs/assets/dashboard-overview.png`.

---

### Task 1: Scaffold `docs/index-v2.html` and fix pre-existing markup bugs

**Files:**
- Create: `docs/index-v2.html` (copy of `docs/index.html`)

**Interfaces:**
- Produces: a working copy of the v1 page at `docs/index-v2.html`, with three known v1 bugs
  fixed, ready for Tasks 2-5 to insert new sections into.

- [ ] **Step 1: Copy v1 to v2**

```bash
cp docs/index.html docs/index-v2.html
```

- [ ] **Step 2: Write verification checks for the bugs this task fixes (expect them to still be broken)**

```bash
# Bug A: hero stat still says 72+ (stale)
grep -c '>72+<' docs/index-v2.html
# Bug B: Antigravity icon has an orphan closing </svg> tag (no matching <svg> open tag before it)
grep -n '<img class="w-8 h-8" src="assets/antigravity-color.svg" alt="Antigravity"></svg>' docs/index-v2.html
# Bug C: "dash.desc" key appears twice per non-English language (5 languages x 2 = 10, +1 for English's single correct entry = 11 total)
grep -c '"dash.desc":' docs/index-v2.html
```

Expected: Bug A prints `1` (still present), Bug B prints one matching line (still present),
Bug C prints `11` (5 languages still duplicated).

- [ ] **Step 3: Fix Bug A — update the hero stat number**

Edit `docs/index-v2.html`, replace:

```html
      <div class="text-2xl font-bold gradient-text">72+</div>
      <div class="text-muted text-xs mt-1" data-i18n="stats.features">Features Shipped</div>
```

with:

```html
      <div class="text-2xl font-bold gradient-text">100+</div>
      <div class="text-muted text-xs mt-1" data-i18n="stats.features">Features Shipped</div>
```

- [ ] **Step 4: Fix Bug B — remove the orphan `</svg>` on the Antigravity icon**

Edit `docs/index-v2.html`, replace:

```html
        <!-- Antigravity -->
        <img class="w-8 h-8" src="assets/antigravity-color.svg" alt="Antigravity"></svg>
```

with:

```html
        <!-- Antigravity -->
        <img class="w-8 h-8" src="assets/antigravity-color.svg" alt="Antigravity">
```

- [ ] **Step 5: Fix Bug C — remove the stale duplicate `dash.desc` line in each of the 5 non-English languages**

Each of `zh-TW`, `zh-CN`, `ja`, `ko`, `es` has two consecutive-ish `"dash.desc"` entries: an
older macOS-only description (delete this one) followed by the current Windows/Linux/Tauri
description (keep this one). Delete the older line in each language block:

```bash
# zh-TW — delete the stale line, keep the Windows/Linux/Tauri one
```

Edit `docs/index-v2.html`, remove this line (and only this line) from the `"zh-TW"` block:
```html
    "dash.desc": "原生 macOS 應用程式，即時監控。觀察 AI agent 工作、審查結果、管理 feature。",
```

Remove this line from the `"zh-CN"` block:
```html
    "dash.desc": "原生 macOS 应用，实时监控。观察 AI agent 工作、审查结果、管理 feature。",
```

Remove this line from the `"ja"` block:
```html
    "dash.desc": "ネイティブ macOS アプリでリアルタイム監視。AI エージェントの動作確認、結果レビュー、feature 管理。",
```

Remove this line from the `"ko"` block:
```html
    "dash.desc": "네이티브 macOS 앱으로 실시간 모니터링. AI 에이전트 작업 확인, 결과 리뷰, feature 관리.",
```

Remove this line from the `"es"` block:
```html
    "dash.desc": "App nativa de macOS para monitoreo en tiempo real. Observa tus agentes IA, revisa resultados y gestiona features.",
```

- [ ] **Step 6: Re-run verification checks — expect fixes confirmed**

```bash
grep -c '>72+<' docs/index-v2.html          # expect 0
grep -c '>100+<' docs/index-v2.html         # expect 1
grep -c 'alt="Antigravity"></svg>' docs/index-v2.html  # expect 0 (orphan tag gone)
grep -c '"dash.desc":' docs/index-v2.html   # expect 6 (one per language)
```

- [ ] **Step 7: Validate markup with tidy (informational — existing v1 may already have warnings, just confirm no NEW errors introduced by this task's edits)**

```bash
tidy -q -e docs/index-v2.html 2>&1 | head -20
```

- [ ] **Step 8: Commit**

```bash
git add docs/index-v2.html
git commit -m "docs: scaffold landing page v2 draft, fix stale stat + markup bugs"
```

---

### Task 2: Add "Multi-repo & Worktree Isolation" section

**Files:**
- Modify: `docs/index-v2.html`

**Interfaces:**
- Consumes: the anchor `<!-- How it works -->` comment (unique in the file) produced by
  Task 1's copy of v1 — this task inserts immediately before it.
- Produces: a new section using i18n namespace `multirepo.*`; leaves the same
  `<!-- How it works -->` anchor intact and unique for Task 3 to insert before again.

- [ ] **Step 1: Write verification check (expect fail — section doesn't exist yet)**

```bash
grep -c '"multirepo.title"' docs/index-v2.html
```

Expected: `0`.

- [ ] **Step 2: Insert the section HTML**

Edit `docs/index-v2.html`, replace:

```html
</section>

<!-- How it works -->
```

with:

```html
</section>

<!-- Multi-repo & Worktree -->
<section class="max-w-4xl mx-auto px-6 py-16">
  <h2 class="text-2xl font-bold text-center mb-4" data-i18n="multirepo.title">Built for Multi-Repo Projects</h2>
  <p class="text-muted text-center mb-8 max-w-2xl mx-auto" data-i18n="multirepo.desc">Workspace can span multiple independent git repos plus shared hub repos. Every feature runs in its own isolated git worktree — concurrent features never step on each other's files.</p>
  <div class="grid sm:grid-cols-3 gap-4 mb-8">
    <div class="feature-card rounded-xl p-5">
      <div class="text-accent text-lg font-bold mb-2" data-i18n="multirepo.p1.title">Independent Repos, One Workspace</div>
      <p class="text-muted text-sm" data-i18n="multirepo.p1.desc">Declare each repo's path and mark shared ones as hub — 4x scopes every feature to only the repos it's allowed to touch.</p>
    </div>
    <div class="feature-card rounded-xl p-5">
      <div class="text-green text-lg font-bold mb-2" data-i18n="multirepo.p2.title">Git Worktree Isolation</div>
      <p class="text-muted text-sm" data-i18n="multirepo.p2.desc">Each feature gets its own linked worktree. No branch switching, no stashing, no collision between features running in parallel.</p>
    </div>
    <div class="feature-card rounded-xl p-5">
      <div class="text-purple text-lg font-bold mb-2" data-i18n="multirepo.p3.title">Scope Guard</div>
      <p class="text-muted text-sm" data-i18n="multirepo.p3.desc">Guardrail checks reject any diff that touches a repo outside the feature's declared scope — before it ever reaches review.</p>
    </div>
  </div>
  <div class="code-block rounded-xl p-6 font-mono text-sm overflow-x-auto">
<pre class="whitespace-pre">{
  <span class="text-accent">"workspace"</span>: {
    <span class="text-accent">"repos"</span>: {
      <span class="text-accent">"core"</span>:    { <span class="text-accent">"path"</span>: <span class="text-orange">"core"</span>,    <span class="text-accent">"hub"</span>: <span class="text-purple">true</span> },
      <span class="text-accent">"gateway"</span>: { <span class="text-accent">"path"</span>: <span class="text-orange">"gateway"</span> },
      <span class="text-accent">"admin"</span>:   { <span class="text-accent">"path"</span>: <span class="text-orange">"admin"</span> },
      <span class="text-accent">"web"</span>:     { <span class="text-accent">"path"</span>: <span class="text-orange">"web"</span> }
    }
  },
  <span class="text-accent">"isolation"</span>: <span class="text-orange">"worktree"</span>
}</pre>
  </div>
</section>

<!-- How it works -->
```

- [ ] **Step 3: Add English i18n keys**

Edit `docs/index-v2.html`, replace:

```html
    "evo.driver.desc": "One command to start the continuous self-improvement loop: mine → evaluate → implement → verify → deploy."
  },
  "zh-TW": {
```

with:

```html
    "evo.driver.desc": "One command to start the continuous self-improvement loop: mine → evaluate → implement → verify → deploy.",
    "multirepo.title": "Built for Multi-Repo Projects",
    "multirepo.desc": "Workspace can span multiple independent git repos plus shared hub repos. Every feature runs in its own isolated git worktree — concurrent features never step on each other's files.",
    "multirepo.p1.title": "Independent Repos, One Workspace",
    "multirepo.p1.desc": "Declare each repo's path and mark shared ones as hub — 4x scopes every feature to only the repos it's allowed to touch.",
    "multirepo.p2.title": "Git Worktree Isolation",
    "multirepo.p2.desc": "Each feature gets its own linked worktree. No branch switching, no stashing, no collision between features running in parallel.",
    "multirepo.p3.title": "Scope Guard",
    "multirepo.p3.desc": "Guardrail checks reject any diff that touches a repo outside the feature's declared scope — before it ever reaches review."
  },
  "zh-TW": {
```

- [ ] **Step 4: Add zh-TW i18n keys**

Edit `docs/index-v2.html`, replace:

```html
    "evo.driver.desc": "一條指令啟動持續自我改進迴路：挖掘 → 評估 → 實作 → 驗證 → 部署。"
  },
  "zh-CN": {
```

with:

```html
    "evo.driver.desc": "一條指令啟動持續自我改進迴路：挖掘 → 評估 → 實作 → 驗證 → 部署。",
    "multirepo.title": "為多 Repo 專案打造",
    "multirepo.desc": "Workspace 可橫跨多個獨立 git repo，再加上共用的 hub repo。每個 feature 都在自己的 git worktree 裡執行 — 平行跑的 feature 之間不會互相干擾。",
    "multirepo.p1.title": "獨立 Repo，統一 Workspace",
    "multirepo.p1.desc": "宣告每個 repo 的路徑，把共用的標成 hub — 4x 會把每個 feature 限定在它被允許碰的 repo 範圍內。",
    "multirepo.p2.title": "Git Worktree 隔離",
    "multirepo.p2.desc": "每個 feature 都有自己的 linked worktree。不用切分支、不用 stash，平行跑的 feature 之間不會互撞。",
    "multirepo.p3.title": "Scope 防護",
    "multirepo.p3.desc": "Guardrail 檢查會擋下任何動到 feature 宣告範圍外 repo 的 diff — 在進 review 之前就攔下來。"
  },
  "zh-CN": {
```

- [ ] **Step 5: Re-run verification check — expect pass**

```bash
grep -c '"multirepo.title"' docs/index-v2.html
```

Expected: `2` (one in `en`, one in `zh-TW`).

- [ ] **Step 6: Commit**

```bash
git add docs/index-v2.html
git commit -m "docs: add Multi-repo & Worktree Isolation section to landing page v2"
```

---

### Task 3: Add "Quality Gates" section

**Files:**
- Modify: `docs/index-v2.html`

**Interfaces:**
- Consumes: the same `<!-- How it works -->` anchor (still unique — Task 2 inserted its
  section before it, this task inserts immediately before it again, landing between the
  Multi-repo section and How It Works).
- Produces: a new section using i18n namespace `quality.*`.

- [ ] **Step 1: Write verification check (expect fail)**

```bash
grep -c '"quality.title"' docs/index-v2.html
```

Expected: `0`.

- [ ] **Step 2: Insert the section HTML**

Edit `docs/index-v2.html`, replace:

```html
</section>

<!-- How it works -->
```

with:

```html
</section>

<!-- Quality Gates -->
<section class="max-w-4xl mx-auto px-6 py-16">
  <h2 class="text-2xl font-bold text-center mb-4" data-i18n="quality.title">Every Claim Needs Evidence</h2>
  <p class="text-muted text-center mb-8 max-w-2xl mx-auto" data-i18n="quality.desc">Acceptance criteria aren't just checked off — each one must carry evidence in verify.json, and a round the guardrail says is unproven can't be accepted.</p>
  <div class="grid sm:grid-cols-3 gap-4 mb-8">
    <div class="feature-card rounded-xl p-5">
      <div class="text-accent text-lg font-bold mb-2" data-i18n="quality.p1.title">AC Evidence Mapping</div>
      <p class="text-muted text-sm" data-i18n="quality.p1.desc">Every acceptance criterion in verify.json needs a passed flag and an evidence trail. 4x check blocks the round if evidence is missing.</p>
    </div>
    <div class="feature-card rounded-xl p-5">
      <div class="text-green text-lg font-bold mb-2" data-i18n="quality.p2.title">Selective Deep Review</div>
      <p class="text-muted text-sm" data-i18n="quality.p2.desc">Deep review picks angles based on which files the diff actually touches, instead of running all review angles on every small change.</p>
    </div>
    <div class="feature-card rounded-xl p-5">
      <div class="text-purple text-lg font-bold mb-2" data-i18n="quality.p3.title">Cross-Verified Testing</div>
      <p class="text-muted text-sm" data-i18n="quality.p3.desc">Tester's verify.json results are cross-checked against actual command exit codes — a claimed PASS with contradicting evidence gets caught automatically.</p>
    </div>
  </div>
  <div class="code-block rounded-xl p-6 font-mono text-sm overflow-x-auto">
<pre class="whitespace-pre">{
  <span class="text-accent">"passed"</span>: <span class="text-purple">true</span>,
  <span class="text-accent">"commands"</span>: [
    { <span class="text-accent">"command"</span>: <span class="text-orange">"make test"</span>, <span class="text-accent">"exitCode"</span>: 0, <span class="text-accent">"summary"</span>: <span class="text-orange">"142 passed"</span> }
  ],
  <span class="text-accent">"ac_results"</span>: [
    { <span class="text-accent">"id"</span>: <span class="text-orange">"AC1"</span>, <span class="text-accent">"passed"</span>: <span class="text-purple">true</span>,
      <span class="text-accent">"evidence"</span>: [<span class="text-orange">"go test ./internal/guard/... -run TestScopeViolation -v"</span>] },
    { <span class="text-accent">"id"</span>: <span class="text-orange">"AC2"</span>, <span class="text-accent">"passed"</span>: <span class="text-purple">true</span>,
      <span class="text-accent">"evidence"</span>: [<span class="text-orange">"make lint: 0 errors"</span>] }
  ]
}</pre>
  </div>
</section>

<!-- How it works -->
```

- [ ] **Step 3: Add English i18n keys**

Edit `docs/index-v2.html`, replace:

```html
    "multirepo.p3.desc": "Guardrail checks reject any diff that touches a repo outside the feature's declared scope — before it ever reaches review."
  },
  "zh-TW": {
```

with:

```html
    "multirepo.p3.desc": "Guardrail checks reject any diff that touches a repo outside the feature's declared scope — before it ever reaches review.",
    "quality.title": "Every Claim Needs Evidence",
    "quality.desc": "Acceptance criteria aren't just checked off — each one must carry evidence in verify.json, and a round the guardrail says is unproven can't be accepted.",
    "quality.p1.title": "AC Evidence Mapping",
    "quality.p1.desc": "Every acceptance criterion in verify.json needs a passed flag and an evidence trail. 4x check blocks the round if evidence is missing.",
    "quality.p2.title": "Selective Deep Review",
    "quality.p2.desc": "Deep review picks angles based on which files the diff actually touches, instead of running all review angles on every small change.",
    "quality.p3.title": "Cross-Verified Testing",
    "quality.p3.desc": "Tester's verify.json results are cross-checked against actual command exit codes — a claimed PASS with contradicting evidence gets caught automatically."
  },
  "zh-TW": {
```

- [ ] **Step 4: Add zh-TW i18n keys**

Edit `docs/index-v2.html`, replace:

```html
    "multirepo.p3.desc": "Guardrail 檢查會擋下任何動到 feature 宣告範圍外 repo 的 diff — 在進 review 之前就攔下來。"
  },
  "zh-CN": {
```

with:

```html
    "multirepo.p3.desc": "Guardrail 檢查會擋下任何動到 feature 宣告範圍外 repo 的 diff — 在進 review 之前就攔下來。",
    "quality.title": "每個驗收標準都要有證據",
    "quality.desc": "Acceptance criteria 不是勾一勾就算過 — 每一項都要在 verify.json 附上證據，guardrail 判定證據不足的回合，reviewer 沒辦法直接放行。",
    "quality.p1.title": "AC Evidence Mapping",
    "quality.p1.desc": "verify.json 裡每個 acceptance criterion 都要有 passed 旗標與證據紀錄。缺證據時 4x check 會擋下這一輪。",
    "quality.p2.title": "Selective Deep Review",
    "quality.p2.desc": "Deep review 依 diff 實際動到的檔案自動挑相關角度，不用每次小改動都跑完整套審查角度。",
    "quality.p3.title": "交叉驗證測試結果",
    "quality.p3.desc": "Tester 回報的 verify.json 結果會跟指令實際的 exit code 交叉比對 — 宣稱 PASS 但證據矛盾的情況會被自動抓出來。"
  },
  "zh-CN": {
```

- [ ] **Step 5: Re-run verification check — expect pass**

```bash
grep -c '"quality.title"' docs/index-v2.html
```

Expected: `2`.

- [ ] **Step 6: Commit**

```bash
git add docs/index-v2.html
git commit -m "docs: add Quality Gates section to landing page v2"
```

---

### Task 4: Add "Cost Visibility" section

**Files:**
- Modify: `docs/index-v2.html`

**Interfaces:**
- Consumes: the same `<!-- How it works -->` anchor (still unique), and the existing image
  asset `assets/dashboard-overview.png` (already referenced elsewhere in the page's Dashboard
  section — safe to reference again).
- Produces: a new section using i18n namespace `costviz.*`.

- [ ] **Step 1: Write verification check (expect fail)**

```bash
grep -c '"costviz.title"' docs/index-v2.html
```

Expected: `0`.

- [ ] **Step 2: Insert the section HTML**

Edit `docs/index-v2.html`, replace:

```html
</section>

<!-- How it works -->
```

with:

```html
</section>

<!-- Cost Visibility -->
<section class="max-w-4xl mx-auto px-6 py-16">
  <h2 class="text-2xl font-bold text-center mb-4" data-i18n="costviz.title">Know What Every Round Costs</h2>
  <p class="text-muted text-center mb-8 max-w-2xl mx-auto" data-i18n="costviz.desc">The dashboard breaks down token usage and duration per phase, so you see exactly where the budget goes before it surprises you.</p>
  <div class="grid sm:grid-cols-3 gap-4 mb-8">
    <div class="feature-card rounded-xl p-5">
      <div class="text-accent text-lg font-bold mb-2" data-i18n="costviz.p1.title">Per-Phase Breakdown</div>
      <p class="text-muted text-sm" data-i18n="costviz.p1.desc">Tokens, cost, and duration for designing, coding, reviewing, testing — tracked separately, not just a total.</p>
    </div>
    <div class="feature-card rounded-xl p-5">
      <div class="text-green text-lg font-bold mb-2" data-i18n="costviz.p2.title">Model-Aware</div>
      <p class="text-muted text-sm" data-i18n="costviz.p2.desc">Mix opus for design with sonnet for review and test — the dashboard shows the cost impact of each choice.</p>
    </div>
    <div class="feature-card rounded-xl p-5">
      <div class="text-purple text-lg font-bold mb-2" data-i18n="costviz.p3.title">No Surprises</div>
      <p class="text-muted text-sm" data-i18n="costviz.p3.desc">Runaway rounds show up immediately in the per-phase view instead of hiding in a single end-of-run total.</p>
    </div>
  </div>
  <img src="assets/dashboard-overview.png" alt="Dashboard cost per phase" class="rounded-xl w-full" style="border:1px solid #30363d;">
  <p class="text-muted text-xs text-center mt-3" data-i18n="costviz.img.caption">Dashboard overview — cost and duration per phase at a glance</p>
</section>

<!-- How it works -->
```

- [ ] **Step 3: Add English i18n keys**

Edit `docs/index-v2.html`, replace:

```html
    "quality.p3.desc": "Tester's verify.json results are cross-checked against actual command exit codes — a claimed PASS with contradicting evidence gets caught automatically."
  },
  "zh-TW": {
```

with:

```html
    "quality.p3.desc": "Tester's verify.json results are cross-checked against actual command exit codes — a claimed PASS with contradicting evidence gets caught automatically.",
    "costviz.title": "Know What Every Round Costs",
    "costviz.desc": "The dashboard breaks down token usage and duration per phase, so you see exactly where the budget goes before it surprises you.",
    "costviz.p1.title": "Per-Phase Breakdown",
    "costviz.p1.desc": "Tokens, cost, and duration for designing, coding, reviewing, testing — tracked separately, not just a total.",
    "costviz.p2.title": "Model-Aware",
    "costviz.p2.desc": "Mix opus for design with sonnet for review and test — the dashboard shows the cost impact of each choice.",
    "costviz.p3.title": "No Surprises",
    "costviz.p3.desc": "Runaway rounds show up immediately in the per-phase view instead of hiding in a single end-of-run total.",
    "costviz.img.caption": "Dashboard overview — cost and duration per phase at a glance"
  },
  "zh-TW": {
```

- [ ] **Step 4: Add zh-TW i18n keys**

Edit `docs/index-v2.html`, replace:

```html
    "quality.p3.desc": "Tester 回報的 verify.json 結果會跟指令實際的 exit code 交叉比對 — 宣稱 PASS 但證據矛盾的情況會被自動抓出來。"
  },
  "zh-CN": {
```

with:

```html
    "quality.p3.desc": "Tester 回報的 verify.json 結果會跟指令實際的 exit code 交叉比對 — 宣稱 PASS 但證據矛盾的情況會被自動抓出來。",
    "costviz.title": "每一輪花多少，一目了然",
    "costviz.desc": "Dashboard 把 token 用量與耗時拆到每個 phase，budget 花在哪裡一眼就看到，不會等到最後才嚇一跳。",
    "costviz.p1.title": "逐 Phase 拆解",
    "costviz.p1.desc": "Designing、coding、reviewing、testing 各自的 token、成本、耗時分開追蹤，不是只給一個總數。",
    "costviz.p2.title": "感知模型差異",
    "costviz.p2.desc": "Design 用 opus、review/test 用 sonnet 混著搭 — dashboard 直接顯示每個選擇對成本的影響。",
    "costviz.p3.title": "不會被總數嚇到",
    "costviz.p3.desc": "失控的某一輪在逐 phase 檢視裡馬上現形，不會被藏在跑完才看到的單一總數裡。",
    "costviz.img.caption": "Dashboard 總覽 — 每個 phase 的成本與耗時一目了然"
  },
  "zh-CN": {
```

- [ ] **Step 5: Re-run verification check — expect pass**

```bash
grep -c '"costviz.title"' docs/index-v2.html
```

Expected: `2`.

- [ ] **Step 6: Commit**

```bash
git add docs/index-v2.html
git commit -m "docs: add Cost Visibility section to landing page v2"
```

---

### Task 5: Enhance Self-Evolution section copy

**Files:**
- Modify: `docs/index-v2.html`

**Interfaces:**
- Consumes: existing `evo.desc` keys in `en` and `zh-TW` blocks (values only — no structural
  change, no new keys, no new cards).
- Produces: updated `evo.desc` text in `en` and `zh-TW`.

- [ ] **Step 1: Write verification check (expect fail)**

```bash
grep -c "consolidate them, keeping prompts lean" docs/index-v2.html
```

Expected: `0`.

- [ ] **Step 2: Update the English `evo.desc` value**

Edit `docs/index-v2.html`, replace:

```html
    "evo.desc": "4x learns from its own runs and continuously improves — mining failures, enriching discoveries, gating by value, and iterating itself.",
```

with:

```html
    "evo.desc": "4x learns from its own runs and continuously improves — mining failures, enriching discoveries, gating by value, and iterating itself. When active learnings pass 30, it automatically uses AI to detect semantic duplicates and consolidate them, keeping prompts lean.",
```

- [ ] **Step 3: Update the zh-TW `evo.desc` value**

Edit `docs/index-v2.html`, replace:

```html
    "evo.desc": "4x 從自身 run 中學習並持續改進 — 挖掘失敗、充實發現、價值把關、自我迭代。",
```

with:

```html
    "evo.desc": "4x 從自身 run 中學習並持續改進 — 挖掘失敗、充實發現、價值把關、自我迭代。當 active learnings 超過 30 條時，會自動用 AI 判斷語意重複並整併，避免 prompt 越養越肥。",
```

- [ ] **Step 4: Re-run verification check — expect pass**

```bash
grep -c "consolidate them, keeping prompts lean" docs/index-v2.html
```

Expected: `1`.

- [ ] **Step 5: Commit**

```bash
git add docs/index-v2.html
git commit -m "docs: enhance Self-Evolution copy with learnings consolidation detail"
```

---

### Task 6: Structural and visual verification

**Files:**
- None created/modified — verification only.

**Interfaces:**
- Consumes: the completed `docs/index-v2.html` from Tasks 1-5.

- [ ] **Step 1: Validate markup with tidy**

```bash
tidy -q -e docs/index-v2.html 2>&1
```

Expected: no new errors beyond whatever pre-existed in v1 (compare against
`tidy -q -e docs/index.html 2>&1` if unsure which warnings are pre-existing).

- [ ] **Step 2: Confirm section order matches the spec**

```bash
grep -n "^<!-- " docs/index-v2.html
```

Expected order includes (among the pre-existing comments):
`Why 4x` → `Multi-repo & Worktree` → `Quality Gates` → `Cost Visibility` → `How it works` →
`Quick Start` → `Configuration Examples` → `Demo` → `Dashboard` → `Supported Runners` →
`Self-Evolution` → `Footer`.

- [ ] **Step 3: Confirm all new i18n keys exist in both `en` and `zh-TW`, and in no other language**

```bash
for key in multirepo.title multirepo.p1.title multirepo.p2.title multirepo.p3.title \
           quality.title quality.p1.title quality.p2.title quality.p3.title \
           costviz.title costviz.p1.title costviz.p2.title costviz.p3.title costviz.img.caption; do
  count=$(grep -c "\"$key\"" docs/index-v2.html)
  echo "$key: $count"
done
```

Expected: every line prints `2` (one `en`, one `zh-TW`).

- [ ] **Step 4: Serve locally and smoke-test both languages**

```bash
cd docs && python3 -m http.server 8123 &
sleep 1
curl -s http://localhost:8123/index-v2.html | grep -c "multirepo.title"
kill %1
```

Expected: the page is served (curl succeeds) and contains the new `data-i18n` attributes.

- [ ] **Step 5: Manual browser check**

Open `docs/index-v2.html` directly in a browser (or via the local server from Step 4).
Confirm:
- The three new sections render between "Why 4x" and "How It Works" with no layout breakage.
- Switching the language selector to 繁體中文 shows the new sections in Traditional Chinese;
  switching to any of 日本語/한국어/Español/简体中文 shows the new sections falling back to
  English (expected — not a bug, per Global Constraints).
- No errors in the browser console.
- The Antigravity runner icon still renders correctly (Task 1's fix didn't break it).

This step has no automated pass/fail — record what you observed.

- [ ] **Step 6: No commit needed** (verification-only task; if Step 5 surfaces issues, fix
  them in the relevant task above and re-commit there).
