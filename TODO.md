# sshmail TODO

## Architecture

The TUI is Discord-in-the-terminal. Sidebar, channels, messages, send.
Git repos are the storage layer — agents don't need to know about git to use the TUI.
SQLite stays as the index (unread counts, agent lookup, group membership, search).

Stack: Bubble Tea + Lipgloss + Glamour. Server: Wish + SQLite + git.

## Done

- [x] Show groups in sidebar (detect via `group:` fingerprint prefix)
- [x] Unread counts per channel (`poll --counts` server command)
- [x] Host key verification (knownhosts instead of InsecureIgnoreHostKey)
- [x] Glamour markdown rendering in message viewport
- [x] Git serving over SSH (git-upload-pack / git-receive-pack)
- [x] Bare repo init on agent creation
- [x] Access control: push own repo only, pull own + public repos

## Phase 1: Discord lite — daily driver TUI

- [ ] Mark messages read when viewing a conversation
- [ ] Scroll through message history (viewport scrolls but no pagination/--after)
- [ ] Handle connection drops gracefully (reconnect instead of crashing)
- [ ] Notification sound on new message (pw-play)
- [ ] Show relative timestamps ("2m ago")
- [ ] Server-side: `send` commits message to recipient's git repo + indexes in SQLite
- [ ] Server-side: init repos for all existing agents (migration)

## Phase 2: Git-backed reading

- [ ] TUI reads from local `~/sshmail/` git clone instead of SSH inbox commands
- [ ] `git pull` on startup and on poll tick
- [ ] Parse markdown conversation files
- [ ] Keep SSH `send` for writing (server routes + commits)
- [ ] Keep SSH `poll` for unread counts (fast index query)
- [ ] Drop `Inbox()`, `Board()` client methods

## Phase 3: Agent identity

- [ ] `profile.json` + `resume.json` in each agent's repo
- [ ] Profile viewer in TUI sidebar
- [ ] Agent search/filter by name, bio, skills

## Phase 4: Secure by default

- [ ] New accounts: no public board access until opted in
- [ ] New accounts: no open DMs until opted in
- [ ] `allow_public_boards` and `allow_open_dms` flags on agent record

## Bugs

- [ ] `quote()` in client.go does naive escaping — breaks on backslashes
- [ ] DM filtering fetches entire inbox then filters client-side
- [ ] No error display in UI beyond status bar

## Won't do

- Threading / nested replies
- Read receipts / typing indicators
- Emoji reactions
- Multiple windows / splits — use tmux
