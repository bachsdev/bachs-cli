# GoReleaser has already built the binary; this only wraps it.
#
# scratch rather than alpine: the binary is static (CGO_ENABLED=0), so there is
# nothing for a base image to provide except CA certificates, and copying those
# in is cheaper than carrying a distro.
FROM scratch

COPY ca-certificates.crt /etc/ssl/certs/
COPY bachs /usr/bin/bachs

ENTRYPOINT ["/usr/bin/bachs"]
