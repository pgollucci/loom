#!/bin/sh
# Git askpass helper - provides authentication token to git
# Used for HTTPS authentication with GitHub/GitLab tokens
#
# When git needs credentials, it calls this script with a prompt as $1
# We respond with the appropriate credential based on the prompt

case "$1" in
    Username*)
        case "${GIT_REPO:-}" in
            *gitlab*) echo "oauth2" ;;
            *)        echo "x-access-token" ;;
        esac
        ;;
    Password*)
        echo "${GIT_TOKEN}"
        ;;
    *)
        echo "${GIT_TOKEN}"
        ;;
esac
