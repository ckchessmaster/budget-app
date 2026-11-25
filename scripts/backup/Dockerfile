FROM alpine:3.20

RUN apk add --no-cache \
      age \
      bash \
      ca-certificates \
      gzip \
      postgresql16-client \
      rclone \
 && addgroup -g 10001 -S backup \
 && adduser -u 10001 -S -G backup backup

COPY --chmod=755 backup.sh /usr/local/bin/backup.sh

# Keep rclone cache and temporary encrypted archives under /tmp so the container
# works cleanly with readOnlyRootFilesystem: true and an emptyDir mounted at /tmp.
ENV HOME=/tmp \
    XDG_CACHE_HOME=/tmp/.cache \
    RCLONE_CONFIG=/etc/rclone/rclone.conf \
    RCLONE_REMOTE=gdrive:HomelabBackups/BudgetApp/ \
    BACKUP_TMPDIR=/tmp

USER 10001:10001

ENTRYPOINT ["/usr/local/bin/backup.sh"]
