SET CGO_ENABLED=0
SET GOOS=linux
SET GOARCH=amd64
go build ../ImgAPICacher.go
move ImgAPICacher ../ImgAPICacher