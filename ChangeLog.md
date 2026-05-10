# ChangeLog for Asika

## v20260510DEV > Unleased
- Add `min_procs` / `max_procs` server config for GOMAXPROCS control at startup and runtime hot-reload
- WebUI settings page adds CPU Threads section for min_procs and max_procs
- All platform bots (Telegram, Discord, Slack, Feishu) display CPU thread config in /config command
- Add notification channel fault alerting: when a notifier fails 3 consecutive times, an alert is sent through all other configured notifiers (excluding the failed one)