package Ascii2d

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	fs "github.com/Miuzarte/FlareSolverr-go"
	"github.com/PuerkitoBio/goquery"
)

const (
	API_HOST         = `https://ascii2d.net`
	API_SEARCH_FILE  = `/search/file`
	API_SEARCH_URL   = `/search/url`
	API_RESULT_COLOR = `/search/color`
	API_RESULT_BOVW  = `/search/bovw`
)

type Client struct {
	Host               string
	FlareSolverrClient *fs.Client
	NumResults         int // TODO: 显式指定返回的结果数量
}

func NewClient(overrideHost string, fsClient *fs.Client) *Client {
	host := overrideHost
	if host == "" {
		host = API_HOST
	} else {
		if !strings.HasPrefix(overrideHost, "http") {
			overrideHost = "https://" + overrideHost
		}
		host = strings.TrimRight(overrideHost, "/")
	}
	return &Client{
		Host:               host,
		FlareSolverrClient: fsClient,
	}
}

func (c *Client) Search(ctx context.Context, image any) (color Result, bovw Result, err error) {
	switch img := image.(type) {
	case string:
		if strings.HasPrefix(img, "http") {
			return c.Get(ctx, img)
		} else {
			// read local
			f, err := os.Open(img)
			if err != nil {
				return Result{}, Result{}, err
			}
			defer f.Close()
			return c.Search(ctx, f)
		}

	case []byte:
		return c.Post(ctx, img)
	case io.Reader:
		imgData, err := io.ReadAll(img)
		if err != nil {
			return Result{}, Result{}, err
		}
		return c.Post(ctx, imgData)

	default:
		return Result{}, Result{}, fmt.Errorf("unsupported image type: %T", image)
	}
}

func (c *Client) Post(ctx context.Context, imgData []byte) (color Result, bovw Result, err error) {
	buf := bytes.Buffer{}
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", "image")
	if err != nil {
		return Result{}, Result{}, err
	}
	_, err = part.Write(imgData)
	if err != nil {
		return Result{}, Result{}, err
	}
	err = writer.Close()
	if err != nil {
		return Result{}, Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Host+API_SEARCH_FILE, &buf)
	if err != nil {
		return Result{}, Result{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	hc := http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // 不跟随重定向
		},
	}
	resp, err := hc.Do(req)
	if err != nil && !errors.Is(err, http.ErrUseLastResponse) {
		return Result{}, Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		return Result{}, Result{}, errors.New(resp.Status)
	}

	colorUrl := resp.Header.Get("Location")
	if colorUrl == "" {
		return Result{}, Result{}, errors.New("empty location")
	}
	bovwUrl := strings.Replace(colorUrl, "/color/", "/bovw/", 1)

	sessionName := "ascii2d_" + time.Now().Format("20060102150405")
	err = c.FlareSolverrClient.SessionsCreate(ctx, sessionName, nil)
	if err != nil {
		return Result{}, Result{}, err
	}
	defer c.FlareSolverrClient.SessionsDestroy(ctx, sessionName)

	wg := sync.WaitGroup{}

	var colorBody, bovwBody string
	var colorErr, bovwErr error
	wg.Go(func() {
		resp, err := c.FlareSolverrClient.Get(ctx, colorUrl, map[string]any{
			fs.PARAM_SESSION:     sessionName,
			fs.PARAM_MAX_TIMEOUT: 60000,
		})
		if err != nil {
			colorErr = err
			return
		}
		colorBody = resp.Solution.Response
	})
	wg.Go(func() {
		resp, err := c.FlareSolverrClient.Get(ctx, bovwUrl, map[string]any{
			fs.PARAM_SESSION:     sessionName,
			fs.PARAM_MAX_TIMEOUT: 60000,
		})
		if err != nil {
			bovwErr = err
			return
		}
		bovwBody = resp.Solution.Response
	})
	wg.Wait()
	if colorErr != nil {
		return Result{}, Result{}, colorErr
	}
	if bovwErr != nil {
		return Result{}, Result{}, bovwErr
	}

	color, colorErr = c.getResult(colorBody, colorUrl)
	if colorErr != nil {
		return Result{}, Result{}, colorErr
	}
	bovw, bovwErr = c.getResult(bovwBody, bovwUrl)
	if bovwErr != nil {
		return Result{}, Result{}, bovwErr
	}

	color.ResultUrl = colorUrl
	bovw.ResultUrl = bovwUrl
	return color, bovw, nil
}

func (c *Client) Get(ctx context.Context, imgUrl string) (color Result, bovw Result, err error) {
	searchUrl := c.Host + API_SEARCH_URL + "/" + imgUrl

	resp, err := c.FlareSolverrClient.Get(ctx, searchUrl, map[string]any{
		fs.PARAM_MAX_TIMEOUT: 60000,
	})
	if err != nil {
		return Result{}, Result{}, err
	}
	colorUrl := resp.Solution.Url
	bovwUrl := strings.ReplaceAll(colorUrl, "/color/", "/bovw/")
	colorBody := resp.Solution.Response
	resp, err = c.FlareSolverrClient.Get(ctx, bovwUrl, map[string]any{
		fs.PARAM_MAX_TIMEOUT: 60000,
	})
	if err != nil {
		return Result{}, Result{}, err
	}
	bovwBody := resp.Solution.Response

	color, err = c.getResult(colorBody, colorUrl)
	if err != nil {
		return Result{}, Result{}, err
	}
	bovw, err = c.getResult(bovwBody, bovwUrl)
	if err != nil {
		return Result{}, Result{}, err
	}

	color.ResultUrl = colorUrl
	bovw.ResultUrl = bovwUrl
	return color, bovw, nil
}

func (c *Client) getResult(body string, resultUrl string) (res Result, err error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return Result{}, err
	}

	var resultType string
	switch {
	case strings.Contains(resultUrl, "/color/"):
		resultType = "color"
	case strings.Contains(resultUrl, "/bovw/"):
		resultType = "bovw"
	}

	found := false

	doc.Find(".item-box").EachWithBreak(func(i int, box *goquery.Selection) bool {
		// external := box.Find(".external") // 人为提交结果
		links := box.Find(".detail-box a")

		// 普通结果
		if links.Length() > 0 {
			titleSel := links.Eq(0)
			authorSel := links.Eq(1)
			thumb, _ := box.Find(".image-box img").Attr("src")

			res = Result{
				Title:     titleSel.Text(),
				Author:    authorSel.Text(),
				Url:       titleSel.AttrOr("href", ""),
				AuthorUrl: authorSel.AttrOr("href", ""),
				Thumbnail: c.Host + thumb,

				ResultUrl:  resultUrl,
				ResultType: resultType,
				Success:    true,
			}
			found = res.Title != ""
			return !found // break if found
		}

		return true // continue
	})

	if !found {
		return Result{}, fmt.Errorf("getResult: failed to parse detail: %s", doc.Text())
	}
	return res, nil
}

type Result struct {
	Title     string
	Author    string
	Url       string
	AuthorUrl string
	Thumbnail string

	ResultUrl  string
	ResultType string // "color" | "bovw"
	Success    bool
}
