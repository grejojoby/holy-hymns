FROM postgres:17-alpine
RUN apk add --no-cache age rclone python3 ca-certificates && addgroup -g 10001 backup && adduser -D -u 10001 -G backup backup
COPY scripts/backup.py /app/backup.py
USER 10001:10001
ENTRYPOINT ["python3", "/app/backup.py"]
CMD ["schedule"]
