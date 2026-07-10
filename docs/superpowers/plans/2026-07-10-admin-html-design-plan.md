# admin.html Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a static web interface `admin.html` in the repository root with premium dark neon glassmorphism styling, metrics cards, split-view tables, and mocked visual entries.

**Architecture:** A self-contained single-page HTML file featuring flexible CSS grid/flex layout, glassmorphic panel styling (blur backdrop, translucid backgrounds), and custom CSS properties matching `client.html`.

**Tech Stack:** HTML5, Vanilla CSS3 (custom variables, grid, flexbox, keyframes), Google Fonts (Inter, Outfit, Fira Code).

## Global Constraints

- Fonts: Import Inter (Body) and Outfit (Heading) and Fira Code (Mono) Google Fonts.
- Backdrop Blur: `backdrop-filter: blur(15px); -webkit-backdrop-filter: blur(15px);` for glassmorphic containers.
- Color codes: deep dark backgrounds (`#07040f`), cyan (`#00f0ff`), violet (`#a78bfa`), rose (`#ff0055`), green/emerald (`#00ff66`), warning/yellow (`#ffaa00`).

---

### Task 1: Style System, Header & Configuration Card

**Files:**
- Create: `admin.html`

**Interfaces:**
- Produces: The HTML structure, CSS design tokens, global styles, page header, and Gateway Credentials Card.

- [ ] **Step 1: Create the basic file and setup HTML head imports & styling system**

Write the basic skeleton of `admin.html` with fonts and design variables.
```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>AgentPay Admin Portal</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&family=Outfit:wght@400;500;600;700;800&family=Fira+Code:wght@400;500&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-color: #07040f;
            --panel-bg: rgba(18, 12, 36, 0.65);
            --card-bg: rgba(20, 10, 35, 0.45);
            --border-color: rgba(139, 92, 246, 0.25);
            --border-hover: rgba(139, 92, 246, 0.5);
            --neon-violet: #a78bfa;
            --neon-violet-glow: rgba(167, 139, 250, 0.35);
            --neon-cyan: #00f0ff;
            --neon-cyan-glow: rgba(0, 240, 255, 0.4);
            --neon-rose: #ff0055;
            --neon-rose-glow: rgba(255, 0, 85, 0.4);
            --neon-green: #00ff66;
            --neon-green-glow: rgba(0, 255, 102, 0.4);
            --neon-yellow: #ffaa00;
            --neon-yellow-glow: rgba(255, 170, 0, 0.4);
            --text-primary: #f8fafc;
            --text-secondary: #cbd5e1;
            --text-muted: #64748b;
            --font-title: 'Outfit', -apple-system, sans-serif;
            --font-body: 'Inter', -apple-system, sans-serif;
            --font-mono: 'Fira Code', monospace;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            background-color: var(--bg-color);
            background-image: 
                radial-gradient(circle at 10% 20%, rgba(91, 33, 182, 0.18) 0%, transparent 45%),
                radial-gradient(circle at 90% 80%, rgba(8, 145, 178, 0.15) 0%, transparent 45%);
            color: var(--text-primary);
            font-family: var(--font-body);
            min-height: 100vh;
            display: flex;
            flex-direction: column;
        }
        .container {
            max-width: 1600px;
            width: 100%;
            margin: 0 auto;
            padding: 1.5rem;
            flex: 1;
            display: flex;
            flex-direction: column;
            gap: 1.5rem;
            position: relative;
            z-index: 1;
        }
    </style>
</head>
<body>
    <div class="container">
        <!-- Will implement header here -->
    </div>
</body>
</html>
```

- [ ] **Step 2: Add Header components**

Add header markup to `admin.html` inside the body:
```html
<header class="admin-header">
    <div class="logo-area">
        <div class="logo-box">AP</div>
        <h1>AgentPay <span class="gradient-text">- Admin Portal</span></h1>
    </div>
    <div class="status-badge">
        <span class="pulse-dot"></span>
        <span class="status-text">Gateway: Connected</span>
    </div>
</header>
```
And add header styling to the `<style>` block:
```css
.admin-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 1.25rem 1.5rem;
    background: rgba(18, 12, 36, 0.4);
    border: 1px solid var(--border-color);
    border-radius: 12px;
    backdrop-filter: blur(15px);
    -webkit-backdrop-filter: blur(15px);
}
.logo-area { display: flex; align-items: center; gap: 0.75rem; }
.logo-box {
    width: 2.25rem;
    height: 2.25rem;
    border-radius: 8px;
    background: linear-gradient(135deg, var(--neon-violet), var(--neon-cyan));
    box-shadow: 0 0 12px var(--neon-violet-glow);
    display: flex;
    align-items: center;
    justify-content: center;
    font-family: var(--font-title);
    font-weight: 800;
    color: var(--bg-color);
}
.admin-header h1 {
    font-family: var(--font-title);
    font-size: 1.35rem;
    font-weight: 700;
}
.gradient-text {
    background: linear-gradient(to right, var(--text-primary), var(--neon-violet));
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
}
.status-badge {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    background: rgba(0, 255, 102, 0.1);
    border: 1px solid var(--neon-green);
    color: var(--neon-green);
    font-size: 0.75rem;
    font-weight: 600;
    padding: 0.35rem 0.75rem;
    border-radius: 20px;
    box-shadow: 0 0 8px rgba(0, 255, 102, 0.25);
}
.pulse-dot {
    width: 6px;
    height: 6px;
    background-color: var(--neon-green);
    border-radius: 50%;
    box-shadow: 0 0 8px var(--neon-green);
    animation: pulse 1.8s infinite;
}
@keyframes pulse {
    0% { transform: scale(0.95); box-shadow: 0 0 0 0 rgba(0, 255, 102, 0.7); }
    70% { transform: scale(1); box-shadow: 0 0 0 6px rgba(0, 255, 102, 0); }
    100% { transform: scale(0.95); box-shadow: 0 0 0 0 rgba(0, 255, 102, 0); }
}
```

- [ ] **Step 3: Add Gateway Credentials Card**

Add credentials inputs inside `admin.html` underneath header:
```html
<section class="credentials-card">
    <h2>API Gateway Connection</h2>
    <div class="form-row">
        <div class="form-group">
            <label for="gateway-url">Gateway URL</label>
            <input type="text" id="gateway-url" value="http://localhost:8080" class="neon-input">
        </div>
        <div class="form-group">
            <label for="admin-secret">Admin Secret Key</label>
            <input type="password" id="admin-secret" value="••••••••••••••••" class="neon-input">
        </div>
        <button class="action-btn save-btn">Save Config</button>
    </div>
</section>
```
And add styling:
```css
.credentials-card {
    background: var(--panel-bg);
    border: 1px solid var(--border-color);
    border-radius: 12px;
    padding: 1.25rem;
    backdrop-filter: blur(15px);
    -webkit-backdrop-filter: blur(15px);
}
.credentials-card h2 {
    font-family: var(--font-title);
    font-size: 1.05rem;
    margin-bottom: 1rem;
    color: var(--neon-violet);
    text-transform: uppercase;
    letter-spacing: 0.05em;
}
.form-row { display: flex; align-items: flex-end; gap: 1rem; flex-wrap: wrap; }
.form-group { flex: 1; min-width: 250px; display: flex; flex-direction: column; gap: 0.4rem; }
.form-group label {
    font-size: 0.75rem;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
}
.neon-input {
    background: rgba(7, 4, 15, 0.6);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    color: var(--text-primary);
    padding: 0.6rem 0.9rem;
    font-family: var(--font-body);
    font-size: 0.9rem;
    outline: none;
    transition: all 0.3s;
}
.neon-input:focus {
    border-color: var(--neon-cyan);
    box-shadow: 0 0 10px rgba(0, 240, 255, 0.25);
}
.action-btn {
    background: linear-gradient(135deg, var(--neon-violet), var(--neon-cyan));
    border: none;
    border-radius: 8px;
    color: var(--bg-color);
    font-family: var(--font-title);
    font-weight: 700;
    padding: 0.65rem 1.25rem;
    cursor: pointer;
    font-size: 0.85rem;
    transition: all 0.3s;
    height: 38px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
}
.action-btn:hover {
    transform: translateY(-1px);
    box-shadow: 0 0 15px rgba(167, 139, 250, 0.4);
}
```

---

### Task 2: KPI Metrics Block, Split View Layout & Mock Data

**Files:**
- Modify: `admin.html`

**Interfaces:**
- Consumes: The basic setup from Task 1.
- Produces: Complete layout of admin.html with 4 KPI cards and split panels (Active Lock Queue, Stripe Sessions) populated with mock values.

- [ ] **Step 1: Implement KPI Cards Grid**

Add HTML for 4 metric cards below credentials:
```html
<section class="kpi-grid">
    <div class="kpi-card" style="--card-glow: var(--neon-cyan-glow)">
        <div class="kpi-label">Total Settled</div>
        <div class="kpi-value">245,620.00 USDC</div>
    </div>
    <div class="kpi-card" style="--card-glow: var(--neon-violet-glow)">
        <div class="kpi-label">Platform Fees</div>
        <div class="kpi-value">1,228.10 USDC</div>
    </div>
    <div class="kpi-card" style="--card-glow: var(--neon-green-glow)">
        <div class="kpi-label">Active Stripe Sessions</div>
        <div class="kpi-value">14</div>
    </div>
    <div class="kpi-card" style="--card-glow: var(--neon-yellow-glow)">
        <div class="kpi-label">Success Rate</div>
        <div class="kpi-value">98.6%</div>
    </div>
</section>
```
And add KPI styling:
```css
.kpi-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 1rem;
}
.kpi-card {
    background: var(--card-bg);
    border: 1px solid rgba(255, 255, 255, 0.05);
    border-radius: 12px;
    padding: 1.25rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    transition: all 0.3s;
    backdrop-filter: blur(15px);
    -webkit-backdrop-filter: blur(15px);
}
.kpi-card:hover {
    border-color: var(--border-hover);
    box-shadow: 0 0 15px var(--card-glow);
}
.kpi-label {
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
}
.kpi-value {
    font-family: var(--font-title);
    font-size: 1.5rem;
    font-weight: 700;
    color: var(--text-primary);
}
```

- [ ] **Step 2: Add Split View Workspace**

Add the split-view containers below metrics:
```html
<div class="dashboard-split">
    <!-- Left Panel: Tasks Queue -->
    <div class="workspace-panel">
        <div class="panel-header">
            <h3>Active Lock Queue</h3>
            <button class="clear-btn neon-rose-border">Clear Queue</button>
        </div>
        <div class="table-container">
            <table>
                <thead>
                    <tr>
                        <th>Lock ID</th>
                        <th>Status</th>
                        <th>Retry</th>
                        <th>Date</th>
                        <th>Actions</th>
                    </tr>
                </thead>
                <tbody>
                    <tr>
                        <td class="mono">lock:0xfa39...10b</td>
                        <td><span class="badge badge-processing">Processing</span></td>
                        <td>0</td>
                        <td>2026-07-10 09:42:15</td>
                        <td><button class="action-link text-rose">Release</button></td>
                    </tr>
                    <tr>
                        <td class="mono">lock:0x1b40...c82</td>
                        <td><span class="badge badge-failed">Failed</span></td>
                        <td>3</td>
                        <td>2026-07-10 09:30:00</td>
                        <td><button class="action-link text-rose">Release</button></td>
                    </tr>
                    <tr>
                        <td class="mono">lock:0x68ef...9a2</td>
                        <td><span class="badge badge-pending">Pending</span></td>
                        <td>1</td>
                        <td>2026-07-10 09:51:22</td>
                        <td><button class="action-link text-rose">Release</button></td>
                    </tr>
                </tbody>
            </table>
        </div>
    </div>

    <!-- Right Panel: Stripe Sessions -->
    <div class="workspace-panel stripe-panel">
        <div class="panel-header">
            <h3>Recent Stripe Sessions</h3>
            <button class="clear-btn stripe-clear">Clear stripe sessions</button>
        </div>
        <ul class="session-list">
            <li>
                <span class="mono">cs_test_a1B2c3...990</span>
                <div class="badge-group">
                    <span class="badge badge-success">Success</span>
                    <button class="delete-icon" aria-label="Delete">✕</button>
                </div>
            </li>
            <li>
                <span class="mono">cs_test_x9Y8z7...124</span>
                <div class="badge-group">
                    <span class="badge badge-success">Success</span>
                    <button class="delete-icon" aria-label="Delete">✕</button>
                </div>
            </li>
            <li>
                <span class="mono">cs_test_m5N6o7...864</span>
                <div class="badge-group">
                    <span class="badge badge-success">Success</span>
                    <button class="delete-icon" aria-label="Delete">✕</button>
                </div>
            </li>
        </ul>
    </div>
</div>
```
And add split grid layout, table/list CSS styling:
```css
.dashboard-split {
    display: grid;
    grid-template-columns: 1.2fr 0.8fr;
    gap: 1.5rem;
}
@media (max-width: 1024px) {
    .dashboard-split { grid-template-columns: 1fr; }
}
.workspace-panel {
    background: var(--panel-bg);
    border: 1px solid var(--border-color);
    border-radius: 12px;
    padding: 1.5rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
    backdrop-filter: blur(15px);
    -webkit-backdrop-filter: blur(15px);
}
.panel-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
    padding-bottom: 0.75rem;
}
.panel-header h3 {
    font-family: var(--font-title);
    font-size: 1.1rem;
    font-weight: 600;
}
.clear-btn {
    background: transparent;
    border: 1px solid var(--neon-rose);
    color: var(--neon-rose);
    border-radius: 6px;
    padding: 0.35rem 0.75rem;
    font-size: 0.75rem;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.3s;
}
.clear-btn:hover {
    background: var(--neon-rose);
    color: var(--bg-color);
    box-shadow: 0 0 10px var(--neon-rose-glow);
}
.table-container { overflow-x: auto; }
table { width: 100%; border-collapse: collapse; text-align: left; font-size: 0.85rem; }
th {
    color: var(--text-muted);
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 0.75rem 0.5rem;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}
td { padding: 0.85rem 0.5rem; border-bottom: 1px solid rgba(255, 255, 255, 0.03); }
.mono { font-family: var(--font-mono); color: var(--neon-cyan); }
.badge {
    display: inline-flex;
    padding: 0.2rem 0.5rem;
    border-radius: 4px;
    font-size: 0.7rem;
    font-weight: 600;
    text-transform: uppercase;
}
.badge-processing { background: rgba(0, 240, 255, 0.15); border: 1px solid var(--neon-cyan); color: var(--neon-cyan); }
.badge-failed { background: rgba(255, 0, 85, 0.15); border: 1px solid var(--neon-rose); color: var(--neon-rose); }
.badge-pending { background: rgba(255, 170, 0, 0.15); border: 1px solid var(--neon-yellow); color: var(--neon-yellow); }
.badge-success { background: rgba(0, 255, 102, 0.15); border: 1px solid var(--neon-green); color: var(--neon-green); }
.action-link {
    background: transparent;
    border: none;
    color: var(--neon-rose);
    cursor: pointer;
    font-weight: 600;
    text-decoration: underline;
    transition: all 0.3s;
}
.action-link:hover { color: #ff5588; text-shadow: 0 0 5px var(--neon-rose-glow); }
.session-list { display: flex; flex-direction: column; gap: 0.75rem; list-style: none; }
.session-list li {
    display: flex;
    justify-content: space-between;
    align-items: center;
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid rgba(255, 255, 255, 0.04);
    border-radius: 8px;
    padding: 0.75rem 1rem;
    transition: all 0.3s;
}
.session-list li:hover {
    border-color: var(--border-hover);
    background: rgba(255, 255, 255, 0.04);
}
.badge-group { display: flex; align-items: center; gap: 0.75rem; }
.delete-icon {
    background: transparent;
    border: none;
    color: var(--text-muted);
    font-size: 0.8rem;
    cursor: pointer;
    transition: all 0.3s;
}
.delete-icon:hover { color: var(--neon-rose); transform: scale(1.15); }
```

---

### Task 3: Execution, Validation & Handoff

**Files:**
- Create: `admin.html`
- Create: `/Users/oraclez/code/AgentPay/.superpowers/sdd/task-2-report.md`

**Interfaces:**
- Consumes: Complete html dashboard.
- Produces: Verified local layout, Git commit of `admin.html`, and completed handoff task report.

- [ ] **Step 1: Assemble and output complete admin.html**
Assemble Task 1 & Task 2 CSS and markup. Output to `/Users/oraclez/code/AgentPay/admin.html`.

- [ ] **Step 2: Commit all changes**
Run git commit to secure files:
```bash
git add admin.html
git commit -m "feat: add premium dark neon glassmorphism admin portal layout"
```

- [ ] **Step 3: Write Completion Report**
Create file `/Users/oraclez/code/AgentPay/.superpowers/sdd/task-2-report.md` specifying details.
