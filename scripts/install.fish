#!/usr/bin/env fish

set -l _version devel
if test (count $argv) -ge 1
  set -l _version $argv[1]
end

set -l _commit (git rev-parse --verify HEAD)

go install -ldflags "-X main.gVersion=$_version -X main.gCommit=$_commit"
