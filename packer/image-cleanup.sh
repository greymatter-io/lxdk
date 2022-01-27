#!/bin/bash
set -e
# Packer leaves intermdiate images behind. They can be identified as the images w/o aliases. See `lxc image list`.
for fp in $(lxc image list --format json | jq -r '.[] | select(.aliases | length == 0) | .fingerprint')
do
    lxc image delete $fp
done

