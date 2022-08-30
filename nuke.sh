#!/bin/bash
#

for c in $(lxc ls | grep lxdk | awk '{print $2}'); do lxc delete -f $c; done


