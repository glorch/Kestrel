<p align="center">
  <img src="https://raw.githubusercontent.com/glorch/Kestrel/main/docs/assets/logo.png" alt="Kestrel Logo" width="120" onerror="this.style.display='none'"/>
</p>

<h1 align="center">🦅 Kestrel</h1>

<p align="center">
  <strong>A lightweight, razor-sharp cloud-native CI/CD engine built with Go.</strong>
</p>

<p align="center">
  <a href="https://github.com/glorch/Kestrel/actions"><img src="https://img.shields.io/github/actions/workflow/status/glorch/Kestrel/ci.yml?branch=main&style=flat-square&logo=github" alt="CI Status" /></a>
  <a href="https://golang.org"><img src="https://img.shields.io/badge/Go-1.23%2B-00ADD8?style=flat-square&logo=go" alt="Go Version" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg?style=flat-square" alt="License" /></a>
  <a href="https://github.com/glorch/Kestrel/releases"><img src="https://img.shields.io/github/v/release/glorch/Kestrel?style=flat-square&color=orange" alt="Release" /></a>
</p>

---

## 📖 Introduction

**Kestrel** (红隼) is a modern, fast, and un-bloated CI/CD workflow orchestrator and runner. 

It functions both as an effortless local pipeline executor (run pipelines locally before pushing to Git) and as an enterprise-grade distributed execution system (Server ⇋ Runner architecture with gRPC/HTTP protocol).

---

## ✨ Key Features

- **⚡ Zero Overhead & Instant Startup**: Single static Go binary without heavy runtimes (no Java JVM, no Python virtualenvs).
- **🕸️ DAG Topological Scheduling**: Automatic dependency resolution and parallel stage batching powered by Kahn's algorithm (`needs: [...]`).
- **🔲 Matrix Build Expansion**: Multi-dimensional matrix build matrix (`matrix: { os: [linux, win], go: [1.22, 1.23] }`) with automatic downstream dependency rewiring and variable substitution.
- **🔀 Conditional Execution (`if: ...`)**: Support for `always()`, `success()`, `failure()`, and environment expressions (`${{ env.BRANCH == 'main' }}`).
- **🔄 Fault-Tolerant Retries**: Configurable job and step-level retry policies (`retries: 2`, `retry-interval: "1s"`) to eliminate flaky network or transient build errors.
- **🚀 Distributed Build Cache (`actions/cache`)**: Archive compression and prefix restore-keys caching for Go build cache, node_modules, and Maven dependencies.
- **🔒 Concurrency Groups & In-Flight Cancellation**: Mutual exclusion locking (`concurrency: { group: "prod-deploy", cancel-in-progress: true }`) preventing race conditions in deployment pipelines.
- **🧩 Declarative Actions Ecosystem**: Reusable step plugins (`uses: actions/setup-go`, `actions/checkout`, `actions/cache`, `actions/upload-artifact`).
- **🐳 Dual Runtime Drivers**:
  - **Docker Engine**: Isolated, reproducible container execution via native Docker SDK with automatic bind-mount workspace.
  - **Host / Shell**: Native execution across Linux, macOS, and Windows PowerShell for maximum raw speed.
- **🛡️ DevSecOps & Supply Chain Security**:
  - **Secrets Log Masking**: Automatic real-time redaction of sensitive credentials, passwords, and tokens (`***`) from stdout/stderr.
  - **CycloneDX 1.5 SBOM**: Automated Software Bill of Materials generation (`kestrel sbom`).
  - **Security Gate Policy**: Threshold enforcement on Critical/High vulnerabilities.
- **🚦 CD Governance, Approval Gates & Change Freeze**:
  - **Manual Approval Gates**: Protects production environments by pausing pipelines at approval gates (`kestrel approvals list / approve`).
  - **Change Freeze Windows**: Policy calendar blocking risky releases during holidays, weekends, or promotions with token bypass (`kestrel freeze list / add`).
  - **Progressive Canary Rollout**: Multi-stage traffic shifting with automated metric evaluation (error rate, p99 latency) and instant auto-rollback on regression.
- **📊 DORA Engineering Metrics**: Automated calculation of the 4 core DevOps delivery metrics (Deployment Frequency, Lead Time, Change Failure Rate, MTTR) with rating tiers (`kestrel metrics dora`).
- **💻 Embedded Modern Web UI Console**: Zero-dependency dark-mode web dashboard embedded directly in the binary (`http://localhost:8080/dashboard`) with real-time logs, approval gates, and DORA charts.
- **📣 ChatOps Multi-Channel Notification Hub**:
  - Unified alert dispatcher supporting Feishu/Lark, WeChat Work (WeCom), DingTalk, and Slack with HMAC signature verification (`kestrel notify send`).
- **🌐 Distributed Server ⇋ Runner Fleet**:
  - Central control plane (`kestrel server start`) with REST API, Webhooks, and task dispatching queue.
  - Distributed runner daemon (`kestrel runner start`) with heartbeat, task polling, and zero-memory-leak chunked disk log streaming.
- **📦 Artifact Collection & Run History**: Automatic file archiving and persistent run history (`kestrel runs list`).
- **🔍 Full CLI Toolchain**:
  - `kestrel run`: Execute pipelines locally or in CI environments.
  - `kestrel lint`: Static analysis for YAML syntax, missing dependencies, and dependency cycles.
  - `kestrel graph`: Print ASCII execution flowcharts or export GitHub-compatible Mermaid diagrams.
  - `kestrel freeze`: Manage deployment freeze windows and policies.
  - `kestrel notify`: Dispatch ChatOps notifications directly from scripts or pipelines.
  - `kestrel metrics`: Inspect DORA engineering productivity and delivery performance.

---

## 🏗️ Architecture

```mermaid
flowchart TB
    subgraph Users ["User / Git Triggers"]
        CLI["kestrel CLI (run / lint / graph)"]
        Webhook["Git Webhook (Push / PR)"]
    end

    subgraph ControlPlane ["Central Control Plane (kestrel server)"]
        API["REST API & Webhook Ingestion"]
        Scheduler["DAG Queue & Dispatcher"]
        Store["Persistent Store (.kestrel/store)"]
        GateMgr["CD Approval Gate Manager"]
    end

    subgraph Workers ["Distributed Worker Fleet (kestrel runner)"]
        Runner1["Runner Node 1 (Docker / Host)"]
        Runner2["Runner Node 2 (Host Native)"]
    end

    CLI -->|Execute local| Scheduler
    Webhook --> API
    API --> Scheduler
    Scheduler --> Store
    Scheduler --> GateMgr
    Scheduler <-->|RPC Task Poll & Log Stream| Workers
```

---

## 🚀 Quick Start

### 1. Installation

#### Build from source (requires Go 1.23+):

```bash
git clone git@github.com:glorch/Kestrel.git
cd Kestrel
go build -o bin/kestrel ./cmd/kestrel

# Verify installation
./bin/kestrel version
```

---

### 2. Define a Pipeline (`.kestrel.yaml`)

Create a `.kestrel.yaml` file in your repository:

```yaml
version: "1.0"
name: "my-service-ci"

env:
  ENVIRONMENT: "staging"

jobs:
  lint:
    name: "Code Linter"
    runs-on: "host"
    commands:
      - "echo 'Running linter...'"

  test:
    name: "Unit Tests"
    runs-on: "host"
    needs: [lint]
    matrix:
      go: ["1.22", "1.23"]
    commands:
      - "echo 'Testing with Go ${{ matrix.go }}'"

  deploy:
    name: "Production Deployment"
    runs-on: "host"
    needs: [test]
    environment: "production"
    approval: true # Requires manual approval before execution
    commands:
      - "echo 'Deployed to production!'"
    artifacts:
      paths:
        - "bin/*"
```

---

### 3. CLI Commands Reference

#### Local Execution
```bash
# Run pipeline locally
kestrel run

# Simulate execution (dry run)
kestrel run --dry-run

# Validate configuration
kestrel lint

# Visualize DAG dependency tree (ASCII or Mermaid)
kestrel graph
kestrel graph --format=mermaid
```

#### Distributed Server & Runner
```bash
# Start central server (Port 8080)
kestrel server start --port 8080

# Start a worker runner connecting to server
kestrel runner start --server http://localhost:8080 --tags host,docker --capacity 2
```

#### CD Approval Gates, Freeze & Notifications
```bash
# List pending deployment approval gates
kestrel approvals list

# Approve a deployment gate
kestrel approvals approve <gate-id> --approver alice --comment "LGTM"

# Register a production change freeze window
kestrel freeze add --name "National Holiday Freeze" --env production --days 7 --bypass-token "EMERGENCY-PASS"

# Send ChatOps notification alert to Feishu / DingTalk / WeCom / Slack
kestrel notify send --channel feishu --webhook https://open.feishu.cn/open-apis/bot/v2/hook/xxx --secret sec123 --title "Deploy Succeeded" --content "Service v2.4.0 live in production"

# Generate CycloneDX 1.5 SBOM
kestrel sbom --out sbom.json

# View past execution runs
kestrel runs list

# Inspect DORA DevOps engineering delivery performance
kestrel metrics dora --days 30
```

#### Modern Web UI Console
Once the server is started with `kestrel server start --port 8080`, simply navigate to:
```text
http://localhost:8080/dashboard
```
In your browser to interactively view the pipeline execution topology, real-time log terminal, approval gate cards, active change freeze windows, and DORA KPI scorecards!

---

## 📁 Repository Structure

```text
Kestrel/
├── cmd/
│   └── kestrel/             # Unified CLI (run, server, runner, approvals, freeze, notify, metrics, sbom)
├── pkg/
│   ├── artifact/            # Artifact archiving and persistence
│   ├── cache/               # Distributed build and dependency cache manager (actions/cache)
│   ├── cd/                  # CD approval gates, canary analyzer, and change freeze calendar
│   ├── dag/                 # Directed Acyclic Graph resolver & visualizers
│   ├── engine/              # Pipeline lifecycle orchestrator with retry scheduling
│   ├── executor/            # Execution drivers (Docker & Host)
│   ├── logger/              # Thread-safe terminal stream logger with secrets masking
│   ├── metrics/             # DORA DevOps engineering performance metrics calculator
│   ├── notify/              # Multi-channel ChatOps notification hub (Feishu, DingTalk, WeCom, Slack)
│   ├── pipeline/            # YAML parser, matrix expansion, condition evaluator, concurrency
│   ├── plugin/              # Declarative step actions & plugin registry (setup-go, checkout, cache)
│   ├── rpc/                 # Server ⇋ Runner distributed RPC protocol
│   ├── runner/              # Distributed runner daemon with streaming log upload
│   ├── security/            # Secrets masking, CycloneDX SBOM, and vulnerability gates
│   ├── server/              # Central control plane, queue scheduler, REST API & embedded Web UI
│   ├── store/               # In-memory, persistent run store and zero-OOM file log store
│   ├── version/             # Build and release metadata
│   └── webhook/             # Git Webhook signature verification and path filtering
├── test/
│   └── e2e_integration_test.go # End-to-end distributed integration tests
├── examples/                # Example pipeline configurations
├── docs/                    # Architecture and enterprise specifications
├── .github/workflows/       # GitHub Actions cross-platform CI
└── .kestrel.yaml            # Self-hosting CI configuration
```

---

## 📄 License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
