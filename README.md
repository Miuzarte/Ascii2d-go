# Ascii2d-go

## 要求

- Go 1.25+
- 运行中的 FlareSolverr 服务端

## 安装

```bash
go get github.com/Miuzarte/Ascii2d-go
```

## 示例

```go
package main

import (
    "context"
    "fmt"
    fs "github.com/Miuzarte/FlareSolverr-go"
    a2d "github.com/Miuzarte/Ascii2d-go"
)

// using `https://ascii2d.net` when empty
const OVERRIDE_HOST = ""

func main() {
    fsClient := fs.NewClient("http://127.0.0.1:8191/v1")
    client := a2d.NewClient(OVERRIDE_HOST, fsClient)

    // can be:
    // url.(string): using .Get()
    // filepath.(string), data.([]byte), r.(io.Reader): using .Post()
    var img any
    color, bovw, err := client.Search(context.Background(), img)
    if err != nil {
        panic(err)
    }

    printResult := func(res a2d.Result) {
        switch res.ResultType {
        case "color":
            fmt.Println("色合検索")
        case "bovw":
            fmt.Println("特徴検索")
        }
        fmt.Printf("thumbnail: %s", res.Thumbnail)
        fmt.Printf("%s - %s\n", res.Title, res.Author)
        fmt.Println(res.Url)
        fmt.Println(res.AuthorUrl)
    }

    printResult(color)
    printResult(bovw)
}
```
