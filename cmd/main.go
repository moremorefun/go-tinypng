package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/parnurzeal/gorequest"

	"github.com/moremorefun/mcommon"
)

// StTinyResp tinypng压缩返回
type StTinyResp struct {
	Error string `json:"error"`
	Input struct {
		Size int    `json:"size"`
		Type string `json:"type"`
	} `json:"input"`
	Output struct {
		Size   int     `json:"size"`
		Type   string  `json:"type"`
		Width  int     `json:"width"`
		Height int     `json:"height"`
		Ratio  float64 `json:"ratio"`
		URL    string  `json:"url"`
	} `json:"output"`
}

// StSourceInfo 文件信息
type StSourceInfo struct {
	Path string
	Info os.FileInfo
}

var cookie = "__stripe_mid=dd15385c-ec85-4b5a-a14a-262a332691a89b12f1; __stripe_sid=9feb8d57-9429-4b14-bd4d-740af2c5becfaf0528;"
var workerSize = 50
var retryLimit = 10
var tinyCount = 0

func main() {
	// 读取运行参数
	var argInputPath = flag.String("i", "./input", "input path")
	var argOutputPath = flag.String("o", "./output", "out path")
	var h = flag.Bool("h", false, "help message")
	flag.Parse()
	if *h {
		flag.Usage()
		return
	}

	*argInputPath = filepath.Clean(*argInputPath)
	*argOutputPath = filepath.Clean(*argOutputPath)

	// 检测输入文件夹
	_, err := os.Stat(*argInputPath)
	if err != nil {
		mcommon.Log.Fatalf("error input path: [%T] %s", err, err.Error())
	}

	var wg sync.WaitGroup
	queue := make(chan *StSourceInfo, workerSize)

	for i := 0; i < workerSize; i++ {
		// 创建workerSize个处理进程
		go func() {
			for {
				sourceInfo := <-queue
				work(*argInputPath, *argOutputPath, sourceInfo, &wg)
			}
		}()
	}

	err = filepath.Walk(*argInputPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			mcommon.Log.Fatalf("err: [%T] %s", err, err.Error())
		}
		wg.Add(1)
		queue <- &StSourceInfo{
			Path: path,
			Info: info,
		}
		return nil
	})
	if err != nil {
		mcommon.Log.Fatalf("err: [%T] %s", err, err.Error())
	}
	wg.Wait()
	mcommon.Log.Debugf("all done tiny count: %d", tinyCount)
}

func work(argInDir, argOutDir string, sourceInfo *StSourceInfo, wg *sync.WaitGroup) {
	defer wg.Done()

	path := sourceInfo.Path
	info := sourceInfo.Info
	if info.IsDir() {
		// 该文件是文件夹
		return
	}
	if strings.HasSuffix(path, ".db") {
		// 该文件是.db结尾
		return
	}
	// 获取文件名
	_, inputFile := filepath.Split(path)
	if strings.HasPrefix(inputFile, ".") {
		// 文件名以.开头
		return
	}
	// 获取输出路径
	outPath := strings.Replace(path, argInDir, argOutDir, 1)
	// 获取输入文件夹路径
	outDir := filepath.Dir(outPath)
	// 检测输出文件夹是否存在
	_, err := os.Stat(outDir)
	if os.IsNotExist(err) {
		// 输出文件夹不存在 创建文件夹
		err = os.MkdirAll(outDir, os.ModePerm)
		if err != nil {
			mcommon.Log.Fatalf("err: [%T] %s", err, err.Error())
		}
	}
	if strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") {
		randomIP := fmt.Sprintf(
			"%d.%d.%d.%d",
			rand.Int31n(253)+1,
			rand.Int31n(253)+1,
			rand.Int31n(253)+1,
			rand.Int31n(253)+1,
		)

		// 是图片
		_, err = os.Stat(outPath)
		if !os.IsNotExist(err) {
			// 图片对应的输出文件存在，也就是已经压缩过
			//mcommon.Log.Debugf("%s file tinyed", path)
			return
		}
		// 读取图片文件
		imgFile, err := os.Open(path)
		if err != nil {
			mcommon.Log.Fatalf("err: [%T] %s", err, err.Error())
		}
		defer func() {
			_ = imgFile.Close()
		}()
		imgFileBs, err := ioutil.ReadAll(imgFile)
		if err != nil {
			mcommon.Log.Fatalf("err: [%T] %s", err, err.Error())
		}
		// 进行压缩
		mcommon.Log.Debugf("%s tiny req start", path)
		tinyReqCount := 0
	GotoTiny:
		tinyStatus, tinyResp, errs := gorequest.New().
			Post("https://tinypng.com/web/shrink").
			Set("Cookie", cookie).
			Set("X-Forwarded-For", randomIP).
			Set("Referer", "https://tinypng.com/").
			Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:82.0) Gecko/20100101 Firefox/82.0").
			Type(gorequest.TypeText).
			Send(string(imgFileBs)).
			EndBytes()
		if errs != nil {
			tinyReqCount++
			if tinyReqCount < retryLimit {
				goto GotoTiny
			}
			mcommon.Log.Fatalf("err: [%T] %s", errs[0], errs[0].Error())
		}
		if tinyStatus.StatusCode != 200 && tinyStatus.StatusCode != 201 {
			if tinyStatus.StatusCode == 429 {
				// 频率过快
				mcommon.Log.Debugf("%s tiny resp sleep wait", path)
				time.Sleep(time.Second * 30)
				goto GotoTiny
			}
			// 状态错误
			tinyReqCount++
			if tinyReqCount < retryLimit {
				goto GotoTiny
			}
			mcommon.Log.Fatalf("tiny http status: %d", tinyStatus.StatusCode)
		}
		mcommon.Log.Debugf("%s tiny resp done", path)
		var resp StTinyResp
		err = json.Unmarshal(tinyResp, &resp)
		if err != nil {
			mcommon.Log.Fatalf("err: [%T] %s", err, err.Error())
		}
		tinyURL := resp.Output.URL
		if tinyURL == "" {
			if resp.Error == "too_many_requests" || resp.Error == "invalid_request" {
				tinyReqCount++
				if tinyReqCount < retryLimit {
					goto GotoTiny
				}
			}
			mcommon.Log.Fatalf("%s tiny resp error: %s", path, tinyResp)
		}
		downCount := 0
	GotoDown:
		imgStatus, imgResp, errs := gorequest.New().
			Get(tinyURL).
			Set("Cookie", cookie).
			Set("X-Forwarded-For", randomIP).
			Set("Referer", "https://tinypng.com/").
			Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:82.0) Gecko/20100101 Firefox/82.0").
			EndBytes()
		if errs != nil {
			downCount++
			if downCount < retryLimit {
				goto GotoDown
			}
			mcommon.Log.Fatalf("err: [%T] %s", errs[0], errs[0].Error())
		}
		if imgStatus.StatusCode != 200 {
			downCount++
			if downCount < retryLimit {
				goto GotoDown
			}
			mcommon.Log.Fatalf("img status: %d", imgStatus.StatusCode)
		}
		err = ioutil.WriteFile(
			outPath,
			imgResp,
			info.Mode(),
		)
		if err != nil {
			mcommon.Log.Fatalf("err: [%T] %s", err, err.Error())
		}
		tinyCount++
		mcommon.Log.Debugf("%s tiny save done", path)
	} else {
		// 拷贝其他文件
		err = CopyFile(path, outPath)
		if err != nil {
			mcommon.Log.Fatalf("err: [%T] %s", err, err.Error())
		}
		//mcommon.Log.Debugf("%s copy no img file done", path)
	}
}

// CopyFile copies a file from src to dst. If src and dst files exist, and are
// the same, then return success. Otherise, attempt to create a hard link
// between the two files. If that fail, copy the file contents from src to dst.
func CopyFile(src, dst string) (err error) {
	sfi, err := os.Stat(src)
	if err != nil {
		return
	}
	if !sfi.Mode().IsRegular() {
		// cannot copy non-regular files (e.g., directories,
		// symlinks, devices, etc.)
		return fmt.Errorf("CopyFile: non-regular source file %s (%q)", sfi.Name(), sfi.Mode().String())
	}
	dfi, err := os.Stat(dst)
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
	} else {
		if !(dfi.Mode().IsRegular()) {
			return fmt.Errorf("CopyFile: non-regular destination file %s (%q)", dfi.Name(), dfi.Mode().String())
		}
		if os.SameFile(sfi, dfi) {
			return
		}
	}
	if err = os.Link(src, dst); err == nil {
		return
	}
	err = copyFileContents(src, dst)
	return
}

// copyFileContents copies the contents of the file named src to the file named
// by dst. The file will be created if it does not already exist. If the
// destination file exists, all it's contents will be replaced by the contents
// of the source file.
func copyFileContents(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return
	}
	err = out.Sync()
	return
}
