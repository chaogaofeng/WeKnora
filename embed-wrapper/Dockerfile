# syntax=docker/dockerfile:1
# 多阶段构建：编译 embed-wrapper（独立 Go module，仅依赖标准库），
# 再用轻量 alpine 运行。运行镜像带 ca-certificates，以便 HTTPS 出站访问
# GoldNet 短信网关与 WeKnora 实例的 /exchange。
FROM golang:1.21-alpine AS build
ARG GOPROXY_ARG
WORKDIR /src
COPY go.mod ./
RUN go mod download 2>/dev/null || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/embed-wrapper .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/embed-wrapper /app/embed-wrapper
EXPOSE 8088
ENV WRAPPER_PORT=:8088
ENV TZ=Asia/Shanghai
CMD ["/app/embed-wrapper"]
