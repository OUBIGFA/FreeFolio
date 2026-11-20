package cloudsync

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
)

func TestSigV4MatchesAWSDocumentedExamples(t *testing.T) {
	const access, secret = "AKIA" + "IOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	base := func() map[string]string {
		return map[string]string{
			"host":                 "examplebucket.s3.amazonaws.com",
			"x-amz-date":           "20130524T000000Z",
			"x-amz-content-sha256": emptySHA256,
		}
	}
	object := base()
	object["range"] = "bytes=0-9"
	got := sigV4("GET", s3Path([]string{"test.txt"}), "", object, emptySHA256, "us-east-1", access, secret)
	want := "AWS4-HMAC-SHA256 Credential=" + access + "/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got != want {
		t.Fatalf("GET 对象的签名不对：\n%s\n%s", got, want)
	}
	query := s3Query(map[string]string{"prefix": "J", "max-keys": "2"})
	if query != "max-keys=2&prefix=J" {
		t.Fatalf("查询串没有按名字排序：%s", query)
	}
	got = sigV4("GET", "/", query, base(), emptySHA256, "us-east-1", access, secret)
	if !strings.HasSuffix(got, "Signature=34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7") {
		t.Fatalf("列举对象的签名不对：%s", got)
	}
	if s3Path([]string{"目录", "a b+c~.json"}) != "/%E7%9B%AE%E5%BD%95/a%20b%2Bc~.json" {
		t.Fatalf("路径编码不对：%s", s3Path([]string{"目录", "a b+c~.json"}))
	}
}

type fakeDAV struct {
	mu           sync.Mutex
	t            *testing.T
	dirs         map[string]bool
	files        map[string][]byte
	omitListETag bool
}

func (f *fakeDAV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if user, password, ok := r.BasicAuth(); !ok || user != "me" || password != "secret" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	path := strings.TrimRight(r.URL.Path, "/")
	parent := path[:strings.LastIndex(path, "/")]
	if data, ok := f.files[path]; ok {
		tag := `"` + hashBytes(data) + `"`
		w.Header().Set("ETag", tag)
		if r.Method == http.MethodGet && r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if wanted := r.Header.Get("If-Match"); wanted != "" && wanted != tag {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
	} else if r.Header.Get("If-Match") != "" {
		w.WriteHeader(http.StatusPreconditionFailed)
		return
	}
	switch r.Method {
	case "MKCOL":
		switch {
		case f.dirs[path]:
			w.WriteHeader(http.StatusMethodNotAllowed)
		case !f.dirs[parent]:
			w.WriteHeader(http.StatusConflict)
		default:
			f.dirs[path] = true
			w.WriteHeader(http.StatusCreated)
		}
	case http.MethodPut:
		if _, exists := f.files[path]; exists && r.Header.Get("If-None-Match") == "*" {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if !f.dirs[parent] {
			w.WriteHeader(http.StatusConflict)
			return
		}
		if r.ContentLength < 0 {
			f.t.Error("上传没有带 Content-Length")
		}
		f.files[path], _ = io.ReadAll(r.Body)
		w.Header().Set("ETag", `"`+hashBytes(f.files[path])+`"`)
		w.WriteHeader(http.StatusCreated)
	case http.MethodGet:
		data, ok := f.files[path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write(data)
	case http.MethodDelete:
		if _, ok := f.files[path]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(f.files, path)
		w.WriteHeader(http.StatusNoContent)
	case "PROPFIND":
		if !f.dirs[path] {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		href := func(p string, dir bool) string {
			u := url.URL{Path: p}
			if dir {
				u.Path += "/"
			}
			return u.EscapedPath()
		}
		var body strings.Builder
		body.WriteString(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">`)
		fmt.Fprintf(&body, `<d:response><d:href>%s</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>`, href(path, true))
		if r.Header.Get("Depth") == "1" {
			for dir := range f.dirs {
				if strings.HasPrefix(dir, path+"/") && !strings.Contains(dir[len(path)+1:], "/") {
					fmt.Fprintf(&body, `<d:response><d:href>%s</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>`, href(dir, true))
				}
			}
			for file, data := range f.files {
				if strings.HasPrefix(file, path+"/") && !strings.Contains(file[len(path)+1:], "/") {
					tag := ""
					if !f.omitListETag {
						tag = "<d:getetag>\"" + hashBytes(data) + "\"</d:getetag>"
					}
					fmt.Fprintf(&body, `<d:response><d:href>%s</d:href><d:propstat><d:prop><d:resourcetype/><d:getcontentlength>%d</d:getcontentlength>%s</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`, href(file, false), len(data), tag)
				}
			}
		}
		body.WriteString(`</d:multistatus>`)
		w.WriteHeader(http.StatusMultiStatus)
		io.WriteString(w, body.String())
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func TestWebDAVStoreCreatesFoldersListsOneLevelAndHandlesChineseFolders(t *testing.T) {
	dav := &fakeDAV{t: t, dirs: map[string]bool{"": true, "/dav": true}, files: map[string][]byte{}}
	server := httptest.NewServer(dav)
	defer server.Close()
	store, err := newWebDAVStore(server.URL+"/dav/我的坚果云/FreeMD/", "me", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Probe(); err == nil {
		t.Fatal("上级目录不存在时测试连接应失败，并提示先建文件夹")
	}
	dav.dirs["/dav/我的坚果云"] = true
	if err := store.Probe(); err != nil {
		t.Fatal(err)
	}
	put := func(key, text string) {
		t.Helper()
		if err := store.Put(key, strings.NewReader(text), int64(len(text))); err != nil {
			t.Fatal(err)
		}
	}
	put("devices/a.json", "{}")
	put("devices/a/1-2.json", "快照")
	if string(dav.files["/dav/我的坚果云/FreeMD/devices/a/1-2.json"]) != "快照" {
		t.Fatalf("文件没有放到同步目录下：%v", dav.files)
	}
	entries, err := store.List("devices")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	if len(entries) != 2 || entries[0] != (remoteEntry{Name: "a", Dir: true}) || entries[1] != (remoteEntry{Name: "a.json", Size: 2, ETag: `"` + hashBytes([]byte("{}")) + `"`}) {
		t.Fatalf("只应列出这一层的目录和文件：%+v", entries)
	}
	if missing, err := store.List("没有的目录"); err != nil || len(missing) != 0 {
		t.Fatalf("不存在的目录应是空的：%v %v", missing, err)
	}
	var got bytes.Buffer
	if err := store.Get("devices/a/1-2.json", &got); err != nil || got.String() != "快照" {
		t.Fatalf("下载：%q %v", got.String(), err)
	}
	if err := store.Get("devices/none.json", io.Discard); err == nil {
		t.Fatal("下载不存在的文件应失败")
	}
	if err := store.Delete("devices/a.json"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("devices/a.json"); err != nil {
		t.Fatalf("删除已经没有的文件也算成功：%v", err)
	}
	wrong, _ := newWebDAVStore(server.URL+"/dav/我的坚果云/FreeMD", "me", "wrong")
	if err := wrong.Probe(); err == nil || !strings.Contains(err.Error(), "密码") {
		t.Fatalf("密码不对时要说清楚：%v", err)
	}
	if _, err := newWebDAVStore("dav.example.com/x", "", ""); err == nil {
		t.Fatal("没有 https:// 的地址应被拒绝")
	}
}

type fakeS3 struct {
	mu           sync.Mutex
	t            *testing.T
	objects      map[string][]byte
	omitListETag bool
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	headers := map[string]string{
		"host":                 r.Host,
		"x-amz-date":           r.Header.Get("x-amz-date"),
		"x-amz-content-sha256": r.Header.Get("x-amz-content-sha256"),
	}
	if value := r.Header.Get("If-None-Match"); value != "" {
		headers["if-none-match"] = value
	}
	for _, name := range []string{"if-match", "content-type"} {
		if value := r.Header.Get(name); value != "" {
			headers[name] = value
		}
	}
	want := sigV4(r.Method, r.URL.EscapedPath(), r.URL.RawQuery, headers, headers["x-amz-content-sha256"], "auto", "key", "secret")
	if r.Header.Get("Authorization") != want {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `<Error><Code>SignatureDoesNotMatch</Code><Message>bad signature</Message></Error>`)
		return
	}
	key, ok := strings.CutPrefix(r.URL.Path, "/bucket/")
	if !ok && r.URL.Path != "/bucket" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if data, ok := f.objects[key]; ok {
		tag := `"` + hashBytes(data) + `"`
		w.Header().Set("ETag", tag)
		if r.Method == http.MethodGet && r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if wanted := r.Header.Get("If-Match"); wanted != "" && wanted != tag {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
	} else if r.Header.Get("If-Match") != "" {
		w.WriteHeader(http.StatusPreconditionFailed)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		prefix := r.URL.Query().Get("prefix")
		type content struct {
			Key  string
			Size int
			ETag string `xml:"ETag,omitempty"`
		}
		type commonPrefix struct{ Prefix string }
		var listing struct {
			XMLName        xml.Name `xml:"ListBucketResult"`
			Contents       []content
			CommonPrefixes []commonPrefix
		}
		seen := map[string]bool{}
		for name, data := range f.objects {
			rest, ok := strings.CutPrefix(name, prefix)
			if !ok {
				continue
			}
			if i := strings.Index(rest, "/"); i >= 0 {
				if dir := prefix + rest[:i+1]; !seen[dir] {
					seen[dir] = true
					listing.CommonPrefixes = append(listing.CommonPrefixes, commonPrefix{dir})
				}
				continue
			}
			tag := ""
			if !f.omitListETag {
				tag = `"` + hashBytes(data) + `"`
			}
			listing.Contents = append(listing.Contents, content{name, len(data), tag})
		}
		xml.NewEncoder(w).Encode(listing)
	case r.Method == http.MethodPut:
		if _, exists := f.objects[key]; exists && r.Header.Get("If-None-Match") == "*" {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		f.objects[key], _ = io.ReadAll(r.Body)
		w.Header().Set("ETag", `"`+hashBytes(f.objects[key])+`"`)
	case r.Method == http.MethodGet:
		data, ok := f.objects[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `<Error><Code>NoSuchKey</Code><Message>no such key</Message></Error>`)
			return
		}
		w.Write(data)
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	}
}

func TestS3StoreSignsRequestsAndKeepsEverythingUnderThePrefix(t *testing.T) {
	s3 := &fakeS3{t: t, objects: map[string][]byte{"别人的/文件.txt": []byte("x")}}
	server := httptest.NewServer(s3)
	defer server.Close()
	settings := Config{
		S3Endpoint: server.URL, S3Region: "auto", S3Bucket: "bucket", S3Prefix: "/备份/FreeMD/",
		S3AccessKey: "key", S3SecretKey: "secret", S3PathStyle: true,
	}
	store, err := newS3Store(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Probe(); err != nil {
		t.Fatal(err)
	}
	text := "快照 with space+plus"
	if err := store.Put("devices/a b/1-2.json", strings.NewReader(text), int64(len(text))); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("devices/a.json", strings.NewReader("{}"), 2); err != nil {
		t.Fatal(err)
	}
	if string(s3.objects["备份/FreeMD/devices/a b/1-2.json"]) != text {
		t.Fatalf("对象没有放到同步目录下：%v", s3.objects)
	}
	entries, err := store.List("devices")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	if len(entries) != 2 || entries[0] != (remoteEntry{Name: "a b", Dir: true}) || entries[1] != (remoteEntry{Name: "a.json", Size: 2, ETag: `"` + hashBytes([]byte("{}")) + `"`}) {
		t.Fatalf("只应列出这一层：%+v", entries)
	}
	var got bytes.Buffer
	if err := store.Get("devices/a b/1-2.json", &got); err != nil || got.String() != text {
		t.Fatalf("下载：%q %v", got.String(), err)
	}
	if err := store.Get("devices/none.json", io.Discard); err == nil || !strings.Contains(err.Error(), "NoSuchKey") {
		t.Fatalf("下载不存在的对象要带上服务端的原因：%v", err)
	}
	if err := store.Delete("devices/a.json"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s3.objects["别人的/文件.txt"]; !ok || len(s3.objects) != 2 {
		t.Fatalf("不应碰同步目录以外的对象：%v", s3.objects)
	}
	settings.S3SecretKey = "wrong"
	wrong, _ := newS3Store(settings)
	if err := wrong.Probe(); err == nil || !strings.Contains(err.Error(), "密钥") {
		t.Fatalf("密钥不对时要说清楚：%v", err)
	}
	settings.S3Bucket = ""
	if _, err := newS3Store(settings); err == nil {
		t.Fatal("没填存储桶应被拒绝")
	}
}

func TestS3StoreDoesNotFollowRedirectsAndNamesTheBucketRegion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-amz-bucket-region", "eu-west-1")
		w.Header().Set("Location", "http://127.0.0.1:1/elsewhere")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer server.Close()
	store, err := newS3Store(Config{S3Endpoint: server.URL, S3Bucket: "bucket", S3AccessKey: "key", S3SecretKey: "secret", S3PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Probe(); err == nil || !strings.Contains(err.Error(), "eu-west-1") {
		t.Fatalf("重定向应提示区域不对：%v", err)
	}
}
