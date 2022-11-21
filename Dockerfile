# builder image
FROM public.ecr.aws/docker/library/golang:1.19 as builder
WORKDIR /workspace
COPY . .
RUN go mod download
RUN make build

# final image
FROM public.ecr.aws/docker/library/alpine:3.16
COPY --from=builder /workspace/addon /bin/addon
USER 65534
ENTRYPOINT ["/bin/addon"]
