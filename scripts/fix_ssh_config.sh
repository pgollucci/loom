#!/bin/bash

# This script fixes SSH configuration issues preventing git remote access

# Ensure ssh-askpass is installed
if ! command -v ssh-askpass &> /dev/null
then
    echo "ssh-askpass could not be found, installing..."
    sudo apt-get update
    sudo apt-get install -y ssh-askpass
fi

# Add known hosts to avoid host key verification failures
KNOWN_HOSTS_FILE="$HOME/.ssh/known_hosts"

# Example host, replace with actual host
HOST="github.com"

# Check if the host is already in known_hosts
if ! ssh-keygen -F $HOST > /dev/null
then
    echo "Adding $HOST to known_hosts"
    ssh-keyscan -H $HOST >> $KNOWN_HOSTS_FILE
fi

# Ensure correct permissions
chmod 600 $KNOWN_HOSTS_FILE

# Print success message
echo "SSH configuration issues fixed. You should now have access to git remote operations."
