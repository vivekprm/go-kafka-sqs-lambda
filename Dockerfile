# -------- BUILD STAGE --------
FROM public.ecr.aws/amazonlinux/amazonlinux:2023 AS build

ENV GOTOOLCHAIN=auto
ENV GOSUMDB=sum.golang.org

RUN dnf install -y \
    gcc \
    gcc-c++ \
    make \
    git \
    go \
    openssl-devel \
    zlib-devel \
    cyrus-sasl-devel

# Build librdkafka (NO make install)
RUN git clone https://github.com/confluentinc/librdkafka.git && \
    cd librdkafka && \
    ./configure --enable-ssl --enable-sasl && \
    make

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build Lambda binary (CGO)
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o /app/bootstrap main.go

# -------- RUNTIME STAGE --------
FROM public.ecr.aws/lambda/provided:al2023

# Copy librdkafka shared libraries ONLY
COPY --from=build /librdkafka/src/librdkafka.so* /usr/lib64/

# Copy Lambda binary
COPY --from=build /app/bootstrap /var/task/bootstrap

CMD ["bootstrap"]