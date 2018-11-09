package mak

import (
	"bytes"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	netURL "net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/golang/protobuf/proto"
	"github.com/json-iterator/go"
	"github.com/vmihailenco/msgpack"
	"golang.org/x/net/html"
)

// Query returns the query param for the provided name.
func (c *Ctx) Query(name string) string {
	if c.query == nil {
		c.query = c.R.URL.Query()
	}
	return c.query.Get(name)
}

// QueryParams returns the query parameters as `url.Values`.
func (c *Ctx) QueryParams() netURL.Values {
	if c.query == nil {
		c.query = c.R.URL.Query()
	}
	return c.query
}

// QueryString returns the raw URL query string.
func (c *Ctx) QueryString() string {
	return c.R.URL.RawQuery
}

// Cookie returns a cookie's value if present and an empty string if not
func (c *Ctx) Cookie(name string) string {
	cookie, err := c.R.Cookie(name)
	if err == nil && cookie != nil {
		return cookie.Value
	}
	return ""
}

// SetCookie sets a new *http.Cookie on the response
func (c *Ctx) SetCookie(name string, cookie *Cookie) {
	cookie.Name = name
	http.SetCookie(c.W, cookie)
}

// GetCookie returns an *http.Cookie matching a name and nil if
// no such cookie exists
func (c *Ctx) GetCookie(name string) *Cookie {
	cookie, err := c.R.Cookie(name)
	if err == nil && cookie != nil {
		return cookie
	}
	return nil
}

// Cookies returns a slice of cookies present in the request
func (c *Ctx) Cookies() []*Cookie {
	return c.R.Cookies()
}

// SetStatus sets the http response's status code
func (c *Ctx) SetStatus(code int) {
	c.W.WriteHeader(code)
}

// ContentType returns the value of the Content-Type header
func (c *Ctx) ContentType() string {
	return c.W.Header().Get("Content-Type")
}

// SetContentType  sets the http response's Content-Type header
func (c *Ctx) SetContentType(ct string) {
	c.W.Header().Set("Content-Type", ct)
}

// GetContentType returns the value of the Content-Type header
func (c *Ctx) GetContentType() string {
	return c.Header("Content-Type")
}

// Header reads a header off the http.Request
func (c *Ctx) Header(name string) string {
	return c.R.Header.Get(name)
}

// GetHeader reads a header off what's already set in (http.ResponseWriter).Header()
func (c *Ctx) GetHeader(name string) string {
	return c.W.Header().Get(name)
}

// SetHeader sets a header
func (c *Ctx) SetHeader(name string, value string) {
	c.W.Header().Set(name, value)
}

// DelHeader removes/deletes a header
func (c *Ctx) DelHeader(name string) {
	c.W.Header().Del(name)
}

// SetHeaderValue sets a header's value(s)
func (c *Ctx) SetHeaderValue(name string, values ...string) {
	if len(values) == 0 {
		return
	}
	c.W.Header()[textproto.CanonicalMIMEHeaderKey(name)] = values
}

// SetHeaderValues sets a header's values
func (c *Ctx) SetHeaderValues(name string, values []string) {
	if len(values) == 0 {
		return
	}
	c.W.Header()[textproto.CanonicalMIMEHeaderKey(name)] = values
}

// SetRawHeader sets a header
func (c *Ctx) SetRawHeader(name string, values []string) {
	c.W.Header()[name] = values
}

// Headers gets the headers
func (c *Ctx) Headers() http.Header {
	return c.R.Header
}

// RemoteAddress returns the last network address that sent the r.
func (c *Ctx) RemoteAddress() string {
	return c.R.RemoteAddr
}

// ClientAddress returns the original network address that sent the r.
func (c *Ctx) ClientAddress() string {
	c.parseClientAddressOnce.Do(c.parseClientAddress)
	return c.clientAddress
}

// parseClientAddress parses the original network address that sent the r into
// the `r.clientAddress`.
func (c *Ctx) parseClientAddress() {
	c.clientAddress = c.RemoteAddress()
	if f := c.Header("forwarded"); f != "" { // See RFC 7239
		for _, p := range strings.Split(strings.Split(f, ",")[0], ";") {
			p := strings.TrimSpace(p)
			if strings.HasPrefix(p, "for=") {
				c.clientAddress = strings.TrimSuffix(
					strings.TrimPrefix(p[4:], "\"["), "]\"",
				)
				break
			}
		}
	} else if xff := c.Header("x-forwarded-for"); xff != "" {
		c.clientAddress = strings.TrimSpace(strings.Split(xff, ",")[0])
	}
}

// WriteContent responds to the client with the content.
func (c *Ctx) WriteContent(content io.ReadSeeker) error {
	if c.Written {
		return nil
	}

	canWrite := false
	var reader io.Reader = content
	defer func() {
		if !canWrite {
			return
		}

		if c.R.TLS != nil && c.GetHeader("strict-transport-security") == "" {
			c.SetHeader("strict-transport-security", "max-age=31536000")
		}

		if reader != nil {
			if c.Status >= 200 && c.Status < 300 &&
				c.Header("accept-ranges") == "" {
				c.SetHeader("accept-ranges", "bytes")
			}

			if reader == content && c.ContentLength == 0 {
				c.ContentLength, _ = content.Seek(
					0,
					io.SeekEnd,
				)
				content.Seek(0, io.SeekStart)
			}
		}

		if c.ContentLength >= 0 &&
			c.GetHeader("content-length") == "" &&
			c.GetHeader("transfer-encoding") == "" &&
			c.Status >= 200 && c.Status != 204 &&
			(c.Status >= 300 || c.R.Method != "CONNECT") {
			c.SetHeader(
				"content-length",
				strconv.FormatInt(c.ContentLength, 10),
			)
		} else {
			c.ContentLength = 0
		}

		c.W.WriteHeader(c.Status)
		if c.R.Method != "HEAD" && reader != nil {
			io.CopyN(c.W, reader, c.ContentLength)
		}

		c.Written = true
	}()

	if c.Status >= 400 { // Something has gone wrong
		canWrite = true
		return nil
	}

	im := c.Header("if-match")
	et := c.GetHeader("etag")
	ius, _ := http.ParseTime(c.Header("if-unmodified-since"))
	lm, _ := http.ParseTime(c.GetHeader("last-modified"))
	if im != "" {
		matched := false
		for {
			im = textproto.TrimString(im)
			if len(im) == 0 {
				break
			}

			if im[0] == ',' {
				im = im[1:]
				continue
			}

			if im[0] == '*' {
				matched = true
				break
			}

			eTag, remain := scanETag(im)
			if eTag == "" {
				break
			}

			if eTagStrongMatch(eTag, et) {
				matched = true
				break
			}

			im = remain
		}

		if !matched {
			c.Status = 412
		}
	} else if !ius.IsZero() && !lm.Before(ius.Add(time.Second)) {
		c.Status = 412
	}

	inm := c.Header("if-none-match")
	ims, _ := http.ParseTime(c.Header("if-modified-since"))
	if inm != "" {
		noneMatched := true
		for {
			inm = textproto.TrimString(inm)
			if len(inm) == 0 {
				break
			}

			if inm[0] == ',' {
				inm = inm[1:]
			}

			if inm[0] == '*' {
				noneMatched = false
				break
			}

			eTag, remain := scanETag(inm)
			if eTag == "" {
				break
			}

			if eTagWeakMatch(eTag, c.GetHeader("etag")) {
				noneMatched = false
				break
			}

			inm = remain
		}

		if !noneMatched {
			if c.R.Method == "GET" || c.R.Method == "HEAD" {
				c.Status = 304
			} else {
				c.Status = 412
			}
		}
	} else if !ims.IsZero() && lm.Before(ims.Add(time.Second)) {
		c.Status = 304
	}

	if c.Status >= 300 && c.Status < 400 {
		if c.Status == 304 {
			c.DelHeader("Content-Type")
			c.DelHeader("content-length")
		}

		canWrite = true

		return nil
	} else if c.Status == 412 {
		return ErrPreConditionFail
	} else if content == nil { // Nothing needs to be written
		canWrite = true
		return nil
	}

	ct := c.GetHeader("Content-Type")
	if ct == "" {
		// Read a chunk to decide between UTF-8 text and binary.
		b := [1 << 9]byte{}
		n, _ := io.ReadFull(content, b[:])
		ct = http.DetectContentType(b[:n])
		if _, err := content.Seek(0, io.SeekStart); err != nil {
			return err
		}

		c.SetContentType(ct)
	}

	size, err := content.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	} else if _, err := content.Seek(0, io.SeekStart); err != nil {
		return err
	}

	c.ContentLength = size

	rh := c.Header("range")
	if rh == "" {
		canWrite = true
		return nil
	} else if c.R.Method == "GET" || c.R.Method == "HEAD" {
		if ir := c.Header("if-range"); ir != "" {
			if eTag, _ := scanETag(ir); eTag != "" &&
				!eTagStrongMatch(eTag, et) {
				canWrite = true
				return nil
			}

			// The If-Range value is typically the ETag value, but
			// it may also be the modtime date. See
			// golang.org/issue/8367.
			if lm.IsZero() {
				canWrite = true
				return nil
			} else if t, _ := http.ParseTime(ir); !t.Equal(lm) {
				canWrite = true
				return nil
			}
		}
	}

	const b = "bytes="
	if !strings.HasPrefix(rh, b) {
		return ErrBadRange.Envoy(c)
	}

	ranges := []httpRange{}
	noOverlap := false
	for _, ra := range strings.Split(rh[len(b):], ",") {
		ra = strings.TrimSpace(ra)
		if ra == "" {
			continue
		}

		i := strings.Index(ra, "-")
		if i < 0 {
			return ErrBadRange.Envoy(c)
		}

		start := strings.TrimSpace(ra[:i])
		end := strings.TrimSpace(ra[i+1:])
		hr := httpRange{}
		if start == "" {
			// If no start is specified, end specifies the range
			// start relative to the end of the file.
			i, err := strconv.ParseInt(end, 10, 64)
			if err != nil {
				return ErrBadRange.Envoy(c)
			}

			if i > size {
				i = size
			}

			hr.start = size - i
			hr.length = size - hr.start
		} else {
			i, err := strconv.ParseInt(start, 10, 64)
			if err != nil || i < 0 {
				return ErrBadRange.Envoy(c)
			}

			if i >= size {
				// If the range begins after the size of the
				// content, then it does not overlap.
				noOverlap = true
				continue
			}

			hr.start = i
			if end == "" {
				// If no end is specified, range extends to end
				// of the file.
				hr.length = size - hr.start
			} else {
				i, err := strconv.ParseInt(end, 10, 64)
				if err != nil || hr.start > i {
					return ErrBadRange.Envoy(c)
				}

				if i >= size {
					i = size - 1
				}

				hr.length = i - hr.start + 1
			}
		}

		ranges = append(ranges, hr)
	}

	if noOverlap && len(ranges) == 0 {
		// The specified ranges did not overlap with the content.
		c.SetHeader("content-range", fmt.Sprintf("bytes */%d", size))
		return ErrBadRange.Envoy(c)
	}

	var rangesSize int64
	for _, ra := range ranges {
		rangesSize += ra.length
	}

	if rangesSize > size {
		ranges = nil
	}

	if l := len(ranges); l == 1 {
		// RFC 2616, section 14.16:
		// "When an HTTP message includes the content of a single range
		// (for example, a response to a request for a single range, or
		// to a request for a set of ranges that overlap without any
		// holes), this content is transmitted with a Content-Range
		// header, and a Content-Length header showing the number of
		// bytes actually transferred.
		// ...
		// A response to a request for a single range MUST NOT be sent
		// using the multipart/byteranges media type."
		ra := ranges[0]
		if _, err := content.Seek(ra.start, io.SeekStart); err != nil {
			c.Status = 416
			return err
		}

		c.ContentLength = ra.length
		c.Status = 206
		c.SetHeader("content-range", ra.contentRange(size))
	} else if l > 1 {
		var w countingWriter
		mw := multipart.NewWriter(&w)
		for _, ra := range ranges {
			mw.CreatePart(ra.header(ct, size))
			c.ContentLength += ra.length
		}

		mw.Close()
		c.ContentLength += int64(w)

		c.Status = 206

		pr, pw := io.Pipe()
		mw = multipart.NewWriter(pw)
		c.SetHeader(
			"Content-Type",
			"multipart/byteranges; boundary="+mw.Boundary(),
		)

		reader = pr
		defer pr.Close()

		go func() {
			for _, ra := range ranges {
				part, err := mw.CreatePart(ra.header(ct, size))
				if err != nil {
					pw.CloseWithError(err)
					return
				}

				if _, err := content.Seek(
					ra.start,
					io.SeekStart,
				); err != nil {
					pw.CloseWithError(err)
					return
				}

				if _, err := io.CopyN(
					part,
					content,
					ra.length,
				); err != nil {
					pw.CloseWithError(err)
					return
				}
			}

			mw.Close()
			pw.Close()
		}()
	}

	canWrite = true

	return nil
}

// WriteBlob responds to the client with the content b.
func (c *Ctx) WriteBlob(b []byte) error {
	return c.WriteContent(bytes.NewReader(b))
}

// WriteString responds to the client with the "text/plain" content s.
func (c *Ctx) WriteString(s string) error {
	c.SetContentType("text/plain; charset=utf-8")
	return c.WriteBlob([]byte(s))
}

// WriteJSON responds to the client with the "application/json" content v.
func (c *Ctx) WriteJSON(v interface{}) error {
	b, err := jsoniter.Marshal(v)
	if err != nil {
		return err
	}

	c.SetContentType("application/json; charset=utf-8")
	return c.WriteBlob(b)
}

// WriteMsgpack responds to the client with the "application/msgpack" content v.
func (c *Ctx) WriteMsgpack(v interface{}) error {
	b, err := msgpack.Marshal(v)
	if err != nil {
		return err
	}

	c.SetContentType("application/msgpack")
	return c.WriteBlob(b)
}

// WriteProtobuf responds to the client with the "application/protobuf" content
// v.
func (c *Ctx) WriteProtobuf(v interface{}) error {
	b, err := proto.Marshal(v.(proto.Message))
	if err != nil {
		return err
	}

	c.SetContentType("application/protobuf")
	return c.WriteBlob(b)
}

// WriteTOML responds to the client with the "application/toml" content v.
func (c *Ctx) WriteTOML(v interface{}) error {
	buf := &bytes.Buffer{}
	if err := toml.NewEncoder(buf).Encode(v); err != nil {
		return err
	}

	c.SetContentType("application/toml; charset=utf-8")
	return c.WriteBlob(buf.Bytes())
}

// WriteXML responds to the client with the "application/xml" content v.
func (c *Ctx) WriteXML(v interface{}) error {
	b, err := xml.Marshal(v)
	if err != nil {
		return err
	}

	c.SetContentType("application/xml; charset=utf-8")
	return c.WriteBlob(append([]byte(xml.Header), b...))
}

// WriteHTML responds to the client with the "text/html" content h.
func (c *Ctx) WriteHTML(h string) error {
	if c.instance != nil && c.instance.Config.AutoPush && c.R.ProtoMajor == 2 {
		tree, err := html.Parse(strings.NewReader(h))
		if err != nil {
			return err
		}

		var f func(*html.Node)
		f = func(n *html.Node) {
			if n.Type == html.ElementNode {
				target := ""
				switch n.Data {
				case "link":
					for _, a := range n.Attr {
						if a.Key == "href" {
							target = a.Val
							break
						}
					}
				case "img", "script":
					for _, a := range n.Attr {
						if a.Key == "src" {
							target = a.Val
							break
						}
					}
				}

				if path.IsAbs(target) {
					c.Push(target, nil)
				}
			}

			for c := n.FirstChild; c != nil; c = c.NextSibling {
				f(c)
			}
		}

		f(tree)
	}

	c.SetHeader("Content-Type", "text/html; charset=utf-8")

	return c.WriteBlob([]byte(h))
}

// WriteFile responds to the client with a file content with the filename.
func (c *Ctx) WriteFile(filename string) error {
	filename, err := filepath.Abs(filename)
	if err != nil {
		return err
	}

	fi, err := os.Stat(filename)
	if err != nil {
		return err
	}

	if fi.IsDir() {
		if p := c.R.URL.EscapedPath(); !hasLastSlash(p) {
			p = path.Base(p) + "/"
			if q := c.R.URL.RawQuery; q != "" {
				p += "?" + q
			}

			c.Status = 301
			return c.Redirect(p)
		}

		filename += "index.html"
		if c.instance.Config.DevMode {
			fmt.Println("attempting to serve index.html...")
		}
	}

	var (
		content io.ReadSeeker
		ct      string
		et      []byte
		mt      time.Time
	)
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err = f.Stat()
	if err != nil {
		return err
	}

	content = f
	mt = fi.ModTime()

	if c.GetHeader("Content-Type") == "" {
		if ct == "" {
			ct = mime.TypeByExtension(filepath.Ext(filename))
		}

		if ct != "" { // Don't worry, someone will check it later
			c.SetHeader("Content-Type", ct)
		}
	}

	if c.GetHeader("etag") == "" {
		if et == nil {
			h := sha256.New()
			if _, err := io.Copy(h, content); err != nil {
				return err
			}

			et = h.Sum(nil)
		}

		c.SetHeader("etag", fmt.Sprintf(`"%x"`, et))
		c.SetHeader("Cache-Control", "private, must-revalidate")
	}

	if c.GetHeader("last-modified") == "" {
		c.SetHeader("last-modified", mt.UTC().Format(http.TimeFormat))
	}

	if strings.Contains(filename, ".html") {
		var raw []byte
		_, err := content.Read(raw)
		if err != nil {
			return err
		}
		return c.WriteHTML(string(raw))
	}
	return c.WriteContent(content)
}

// Redirect responds to the client with a redirection to the url.
func (c *Ctx) Redirect(url string) error {
	if c.Status < 300 || c.Status >= 400 {
		c.Status = 302
	}

	// If the url was relative, make its path absolute by combining with the
	// request path. The client would probably do this for us, but doing it
	// ourselves is more reliable.
	// See RFC 7231, section 7.1.2.
	if u, err := netURL.Parse(url); err != nil {
		return err
	} else if u.Scheme == "" && u.Host == "" {
		if url == "" || url[0] != '/' {
			// Make relative path absolute.
			od, _ := path.Split(c.R.URL.Path)
			url = od + url
		}

		query := ""
		if i := strings.Index(url, "?"); i != -1 {
			url, query = url[:i], url[i:]
		}

		// Clean up but preserve trailing slash.
		trailing := strings.HasSuffix(url, "/")
		url = path.Clean(url)
		if trailing && !strings.HasSuffix(url, "/") {
			url += "/"
		}

		url += query
	}

	c.SetHeader("location", url)
	if c.ContentType() != "" {
		return c.WriteContent(nil)
	}

	// RFC 7231 notes that a short HTML body is usually included in the
	// response because older user agents may not understand status 301 and
	// 307.
	if c.R.Method == "GET" || c.R.Method == "HEAD" {
		c.SetContentType("text/html; charset=utf-8")
	}

	// Shouldn't send the body for POST or HEAD; that leaves GET.
	var body io.ReadSeeker
	if c.R.Method == "GET" {
		body = strings.NewReader(fmt.Sprintf(
			"<a href=\"%s\">%s</a>.\n",
			template.HTMLEscapeString(url),
			strings.ToLower(http.StatusText(c.Status)),
		))
	}

	return c.WriteContent(body)
}

// Push initiates an HTTP/2 server push. This constructs a synthetic request
// using the target and headers, serializes that request into a PUSH_PROMISE
// frame, then dispatches that request using the server's request handlec. The
// target must either be an absolute path (like "/path") or an absolute URL
// that contains a valid host and the same scheme as the parent request. If the
// target is a path, it will inherit the scheme and host of the parent request.
// The headers specifies additional promised request headers. The headers
// cannot include HTTP/2 pseudo headers like ":path" and ":scheme", which
// will be added automatically.
func (c *Ctx) Push(target string, headers http.Header) error {
	p, ok := c.W.(http.Pusher)
	if !ok {
		return nil
	}

	var pos *http.PushOptions
	if l := len(headers); l > 0 {
		pos = &http.PushOptions{
			Header: make(http.Header, l),
		}

		pos.Header.Set("Cache-Control", "private, must-revalidate")

		for name, values := range headers {
			pos.Header[textproto.CanonicalMIMEHeaderKey(name)] = values
		}
	}

	if c.instance.Config.DevMode {
		fmt.Println("http2 asset pushed: ", target)
	}

	return p.Push(target, pos)
}

// scanETag determines if a syntactically valid ETag is present at s. If so, the
// ETag and remaining text after consuming ETag is returned. Otherwise, it
// returns "", "".
func scanETag(s string) (eTag string, remain string) {
	s = textproto.TrimString(s)
	start := 0
	if strings.HasPrefix(s, "W/") {
		start = 2
	}

	if len(s[start:]) < 2 || s[start] != '"' {
		return "", ""
	}

	// ETag is either W/"text" or "text".
	// See RFC 7232, section 2.3.
	for i := start + 1; i < len(s); i++ {
		c := s[i]
		switch {
		// Character values allowed in ETags.
		case c == 0x21 || c >= 0x23 && c <= 0x7E || c >= 0x80:
		case c == '"':
			return string(s[:i+1]), s[i+1:]
		default:
			return "", ""
		}
	}

	return "", ""
}

// eTagStrongMatch reports whether the a and the b match using strong ETag
// comparison.
func eTagStrongMatch(a, b string) bool {
	return a == b && a != "" && a[0] == '"'
}

// eTagWeakMatch reports whether the a and the b match using weak ETag
// comparison.
func eTagWeakMatch(a, b string) bool {
	return strings.TrimPrefix(a, "W/") == strings.TrimPrefix(b, "W/")
}

// httpRange specifies the byte range to be sent to the client.
type httpRange struct {
	start, length int64
}

// contentRange return a Content-Range header of the c.
func (r httpRange) contentRange(size int64) string {
	return fmt.Sprintf("bytes %d-%d/%d", r.start, r.start+r.length-1, size)
}

// header return  the MIME header of the c.
func (r httpRange) header(contentType string, size int64) textproto.MIMEHeader {
	return textproto.MIMEHeader{
		"Content-Range": {r.contentRange(size)},
		"Content-Type":  {contentType},
	}
}

// countingWriter counts how many bytes have been written to it.
type countingWriter int64

// Write implements the `io.Writer`.
func (w *countingWriter) Write(p []byte) (int, error) {
	*w += countingWriter(len(p))
	return len(p), nil
}

// Write implements the `io.Writer`.
func (c *Ctx) Write(b []byte) (int, error) {
	if !c.Written {
		c.ContentLength = -1
		if err := c.WriteContent(nil); err != nil {
			return 0, err
		}

		c.ContentLength = 0
	}

	n, err := c.W.Write(b)
	c.ContentLength += int64(n)

	return n, err
}

// Bind interprets/parses/unmarshalls either the Request body (POST) or other Params (GET).
func (c *Ctx) Bind(v interface{}) error {
	if c.R.Method == "GET" {
		return bindParams(v, c.Params())
	} else if c.R.Body == nil {
		return ErrRequestBodyEmpty.Envoy(c)
	}

	mt, _, err := mime.ParseMediaType(c.GetContentType())
	if err != nil {
		return err
	}

	switch mt {
	case "application/json":
		err = jsoniter.NewDecoder(c.R.Body).Decode(v)
	case "application/msgpack", "application/x-msgpack":
		err = msgpack.NewDecoder(c.R.Body).Decode(v)
	case "application/protobuf", "application/x-protobuf":
		if b, err := ioutil.ReadAll(c.R.Body); err == nil {
			err = proto.Unmarshal(b, v.(proto.Message))
		}
	case "application/toml", "application/x-toml":
		_, err = toml.DecodeReader(c.R.Body, v)
	case "application/xml":
		err = xml.NewDecoder(c.R.Body).Decode(v)
	case "application/x-www-form-urlencoded", "multipart/form-data":
		err = bindParams(v, c.Params())
	default:
		return ErrUnsupportedMediaType.Envoy(c)
	}

	return err
}

// bindParams binds the params into the v.
func bindParams(v interface{}, params []*RequestParam) error {
	typ := reflect.TypeOf(v).Elem()
	if typ.Kind() != reflect.Struct {
		return errors.New("binding element must be a struct")
	}

	val := reflect.ValueOf(v).Elem()
	for i := 0; i < typ.NumField(); i++ {
		vf := val.Field(i)
		if !vf.CanSet() {
			continue
		}

		vfk := vf.Kind()
		if vfk == reflect.Struct {
			err := bindParams(vf.Addr().Interface(), params)
			if err != nil {
				return err
			}

			continue
		}

		tf := typ.Field(i)

		var pv *RequestParamValue
		for _, p := range params {
			if p.Name == tf.Name {
				pv = p.Value()
				break
			}
		}

		if pv == nil {
			continue
		}

		switch tf.Type.Kind() {
		case reflect.Bool:
			b, _ := pv.Bool()
			vf.SetBool(b)
		case reflect.Int,
			reflect.Int8,
			reflect.Int16,
			reflect.Int32,
			reflect.Int64:
			i64, _ := pv.Int64()
			vf.SetInt(i64)
		case reflect.Uint,
			reflect.Uint8,
			reflect.Uint16,
			reflect.Uint32,
			reflect.Uint64:
			ui64, _ := pv.Uint64()
			vf.SetUint(ui64)
		case reflect.Float32, reflect.Float64:
			f64, _ := pv.Float64()
			vf.SetFloat(f64)
		case reflect.String:
			vf.SetString(pv.String())
		default:
			return ErrIndeterminateData
		}
	}

	return nil
}
