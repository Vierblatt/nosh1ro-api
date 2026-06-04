.PHONY: build build-linux deploy

# 本地运行
run:
	go run main.go

# 交叉编译 Linux amd64 二进制
build-linux:
	GOOS=linux GOARCH=amd64 go build -o server main.go

# 上传到华为云服务器
deploy: build-linux
	scp server root@139.159.232.200:/opt/blog-api/server
	ssh root@139.159.232.200 "systemctl restart blog-api && systemctl status blog-api --no-pager"
