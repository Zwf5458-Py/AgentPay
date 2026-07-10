# Task 2 Brief: Create admin.html UI Layout & Design System

**Goal**: Create a static web interface `admin.html` with a premium dark neon glassmorphism style consistent with `client.html`.

**Files**:
- Create: `admin.html`

**Instructions**:
1. Scaffold `admin.html` in the repository root directory.
2. In the style system:
   - Fonts: Import Inter and Outfit google fonts.
   - Core Styling Variables: Define colors for deep dark purple/black backgrounds (`#07040f`), border glows, neon cyan (`#00f0ff`), neon violet (`#ff00ff`), neon green/emerald (`#00ff66`), neon rose (`#ff0055`), and warnings/yellow (`#ffaa00`).
   - Use glassmorphic card design (`backdrop-filter: blur(15px); background: rgba(20, 10, 35, 0.45)`).
3. DOM Structure:
   - Header: Displaying "AgentPay - Admin Portal" and a Connection Status Badge with an indicator light.
   - Credentials Card: Input box for API Gateway URL (default `http://localhost:8080`) and Admin Secret Key (type `password`).
   - KPI Dashboard block:
     - 4 Glowing Cards:
       - Total Settled (USDC)
       - Total Platform Fees (USDC)
       - Active Stripe Sessions
       - Success Rate (%)
   - Split view Layout:
     - Left panel (Tasks Queue): A Table exhibiting Lock ID, Status, Retry count, Date, and Actions column. Provide a "Clear Queue" button at table header.
     - Right panel (Stripe Sessions): A list displaying nuclear-styled consumed Stripe Session IDs and a "Clear stripe sessions" button at list header.
4. Render static mocked values in both KPI blocks and tables as placeholders to evaluate visual correctness.
5. Commit changes.
