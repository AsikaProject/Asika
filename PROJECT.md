## Architecture

```mermaid
graph TB
    subgraph External["External Systems"]
        GH[GitHub / GHE API]
        GL[GitLab API]
        GT[Gitea API]
        FJ[Forgejo / Codeberg API]
        BB[Bitbucket API]
        WH[Webhooks]
    end

    subgraph CLI["CLI (asika)"]
        CM[Cobra Commands]
        SU[Self-Update]
    end

    subgraph Daemon["Daemon (asikad)"]
        subgraph HTTP["HTTP Server (gin)"]
            MW[Middleware Chain]
            RO[API Routes]
        end

        subgraph Core["Core"]
            QC[Queue Manager]
            SY[Syncer]
            EC[Event Consumer]
            LB[Labeler]
        end

        subgraph Platforms["Platform Clients"]
            PC_GH[GitHub / GHE Client]
            PC_GL[GitLab Client]
            PC_GT[Gitea Client]
            PC_FJ[Forgejo / Codeberg Client]
            PC_BB[Bitbucket Client]
        end

        subgraph Events["Event Bus"]
            EB[Event Publisher/Subscriber]
        end

        subgraph Notify["Notifications"]
            SMTP[SMTP]
            TG[Telegram Bot]
            FS[Feishu Bot]
            DC[Discord Bot]
        end

        subgraph DB["Storage"]
            BDB[(bbolt Database)]
        end
    end

    GH --> PC_GH
    GL --> PC_GL
    GT --> PC_GT
    FJ --> PC_FJ
    BB --> PC_BB
    WH --> MW

    CM --> RO
    SU --> GH

    MW --> RO
    RO --> QC
    RO --> SY
    RO --> EC

    QC --> EB
    SY --> EB
    EB --> EC
    EC --> LB
    EC --> QC

    PC_GH --> SY
    PC_GL --> SY
    PC_GT --> SY
    PC_FJ --> SY
    PC_BB --> SY

    QC --> SMTP
    QC --> TG
    QC --> FS
    QC --> DC

    SY --> BDB
    QC --> BDB
    EC --> BDB
```

## Development

### Project Structure

```mermaid
graph LR
    subgraph cmd["cmd/"]
        ASIKA[asika/ → CLI binary]
        ASIKAD[asikad/ → Daemon binary]
    end

    subgraph common["common/"]
        CONFIG[config/ → Config loading/validation, platform helpers]
        DB[db/ → bbolt wrapper]
        PLAT[platforms/ → GitHub/GitLab/Gitea/Forgejo/Bitbucket clients + helpers]
        MODELS[models/ → Data structures]
        EVENTS[events/ → Event bus]
        AUTH[auth/ → JWT, password hash]
        GIT[gitutil/ → Pure Go git ops]
        CI[ci/ → CI provider detection]
        NOTIF[notifier/ → Notification channels]
        VER[version/ → Version info]
    end

    subgraph daemon["daemon/"]
        SRV[server/ → HTTP server, middleware, bootstrap]
        HAND[handlers/ → API routes]
        QUEUE[queue/ → Merge queue state machine]
        SYNC[syncer/ → Cross-platform sync]
        CONS[consumer/ → Event consumer]
        LABEL[labeler/ → Label rule engine]
        POLL[polling/ → Polling mode]
        TPL[templates/ → Web UI templates]
        BOTS[platform/ → Telegram/Feishu bots]
        HOOKS[hooks/ → Git hook runner]
        STALE[stale/ → Stale PR management]
    end

    subgraph lib["lib/"]
        LIB_CMD[commands/ → CLI command handlers]
    end

    ASIKA --> LIB_CMD
    ASIKAD --> SRV
    SRV --> HAND
    SRV --> QUEUE
    HAND --> SYNC
    HAND --> CONS
    CONS --> LABEL
    CONS --> QUEUE
```

### Running Tests

```bash
# All tests
go test ./common/... ./lib/... ./daemon/...

# Specific package
go test ./common/config/...

# Specific test
go test ./common/config -run TestLoad
```

### Build Commands

```bash
# Build both binaries
go build -o asika ./cmd/asika
go build -o asikad ./cmd/asikad

# With version info
go build -ldflags="-X 'asika/lib/commands.Version=v1.0.0'" -o asika ./cmd/asika

# Clean up
rm -f asika asikad
```
