cd "$(dirname $0)/.."
pycodestyle $(./ci/list_tracked_gofiles.sh)