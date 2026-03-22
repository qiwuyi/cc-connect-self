#!/bin/bash
# Setup upstream remotes for cc-connect-self
# Run this after cloning: bash setup-remotes.sh

git remote add upstream https://github.com/chenhg5/cc-connect.git
git remote add upstream-memory https://github.com/ashwinyue/cc-connect-memory.git

echo "Remotes configured:"
git remote -v
