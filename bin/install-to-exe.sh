#!/bin/bash
set -e

host="$1"
[[ -z "$host" ]] && {
    echo "usage: $0 <hostname>"
    exit 1
}
[[ "$host" != *.* ]] && host="$host.exe.xyz"

make build-linux-x86
cat bin/percy-linux-x86 | ssh "$host" "sudo mv /usr/local/bin/percy /usr/local/bin/percy.old; sudo tee /usr/local/bin/percy > /dev/null && sudo chmod +x /usr/local/bin/percy && sudo systemctl restart percy"
