#!/usr/bin/env bash
set -euo pipefail
sed -i.bak "s/func (s \*Store) Sum()/func (s *Store) Total()/" store.go && rm -f store.go.bak
sed -i.bak "s/s.Sum()/s.Total()/" report.go && rm -f report.go.bak
