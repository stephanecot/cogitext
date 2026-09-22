---
id: notes/2026-09-22-ssh-controlpersist-master-held-git-pipes
title: "SSH ControlPersist master held git pipes"
status: open
date: 2026-09-22
author: stephanecot
tags:
  - ssh
  - git
  - hang
---

Dead end found while dogfooding: runGit waited for git stdout and stderr to close, but the SSH ControlPersist master inherits them and lives on after git exits, so init, sync, add and push hung until the master died. The context timeout could not help. Fixed with cmd.WaitDelay, and exec.ErrWaitDelay treated as success.
