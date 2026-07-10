# Design Specification: admin.html UI Layout & Design System

This document outlines the visual identity, DOM hierarchy, and interactive design requirements for `admin.html`.

## 1. Visual Identity & Design Tokens

To maintain a consistent style with `client.html`, `admin.html` will implement a **premium dark neon glassmorphism** style using custom CSS properties:

### CSS Variables
* **Backgrounds & Panels**:
  * `--bg-color`: `#07040f` (deep dark purple/black background)
  * `--panel-bg`: `rgba(18, 12, 36, 0.65)` (translucent dark purple overlay)
  * `--card-bg`: `rgba(20, 10, 35, 0.45)` (glassmorphic card fill)
* **Neon Glow Borders & Colors**:
  * `--neon-violet`: `#a78bfa` (primary brand accent)
  * `--neon-violet-glow`: `rgba(167, 139, 250, 0.35)`
  * `--neon-cyan`: `#00f0ff` (secondary neon cyan accent)
  * `--neon-cyan-glow`: `rgba(0, 240, 255, 0.4)`
  * `--neon-rose`: `#ff0055` (alert/delete rose)
  * `--neon-rose-glow`: `rgba(255, 0, 85, 0.4)`
  * `--neon-green`: `#00ff66` (success green/emerald)
  * `--neon-green-glow`: `rgba(0, 255, 102, 0.4)`
  * `--neon-yellow`: `#ffaa00` (warning/pending orange-yellow)
  * `--neon-yellow-glow`: `rgba(255, 170, 0, 0.4)`
* **Borders & Gradients**:
  * `--border-color`: `rgba(139, 92, 246, 0.25)`
  * `--border-hover`: `rgba(139, 92, 246, 0.5)`
* **Typography**:
  * Title font: `'Outfit', -apple-system, sans-serif`
  * Body font: `'Inter', -apple-system, sans-serif`
  * Monospace font: `'Fira Code', 'Courier New', monospace`

### Backdrop Blur
All panels and cards use `backdrop-filter: blur(15px); -webkit-backdrop-filter: blur(15px);` to render glassmorphism.

---

## 2. Layout & DOM Structure

### A. Header Component
* **Left**: Brand logo and Title "AgentPay - Admin Portal" using a text gradient.
* **Right**: Connection Status Badge:
  * Text "Gateway: Offline" / "Gateway: Connected"
  * A pulsing CSS dot indicating connection status (`--neon-rose` for offline, `--neon-green` for connected).

### B. Credentials Configuration Card
* Input field for **API Gateway URL** (text, default value: `http://localhost:8080`).
* Input field for **Admin Secret Key** (password field for privacy).
* Connect/Save button with neon violet gradient hover effect.

### C. KPI Dashboard block
A 4-column responsive grid containing stats:
1. **Total Settled**: Displaying mock amount (e.g. `245,620.00 USDC`) with a cyan neon label.
2. **Total Platform Fees**: Displaying mock amount (e.g. `1,228.10 USDC`) with violet neon label.
3. **Active Stripe Sessions**: Mock count (e.g. `14`) with green neon label.
4. **Success Rate**: Mock percentage (e.g. `98.6%`) with warning/yellow gradient indicator.

### D. Split-View Content Panels
A responsive grid splitting the workspace into a 3:2 layout:

#### Left Panel: Tasks Queue (Lock Queue Manager)
* **Header**: Table title "Active Lock Queue" and a "Clear Queue" button (neon rose/red alert button).
* **Table**:
  * **Headers**: Lock ID, Status, Retry Count, Date, Actions.
  * **Rows**: Render 3 static mock entries.
  * **Actions**: "Unlock" / "Force Release" operations.

#### Right Panel: Stripe Sessions
* **Header**: List title "Recent Stripe Sessions" and a "Clear stripe sessions" button (neon rose/red outline button).
* **List**:
  * Render 3 mockup entries representing Stripe Session IDs.
  * Styled with a "nuclear/terminal" retro vibe: monospace text, neon green success pills, and close buttons to discard sessions.

---

## 3. Mock Data Details

To verify design aesthetics before dynamic implementation, the following values are hardcoded in the HTML:
* **Tasks Table**:
  1. `lock:0xfa39...10b` | `PROCESSING` (neon cyan badge) | `0` | `2026-07-10 09:42:15` | Action button
  2. `lock:0x1b40...c82` | `FAILED` (neon rose badge) | `3` | `2026-07-10 09:30:00` | Action button
  3. `lock:0x68ef...9a2` | `PENDING` (neon yellow badge) | `1` | `2026-07-10 09:51:22` | Action button
* **Stripe Sessions**:
  1. `cs_test_a1B2c3...990` | `SUCCESS` badge | Delete button
  2. `cs_test_x9Y8z7...124` | `SUCCESS` badge | Delete button
  3. `cs_test_m5N6o7...864` | `SUCCESS` badge | Delete button
