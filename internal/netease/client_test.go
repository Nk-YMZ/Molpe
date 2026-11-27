package netease

import (
	"crypto/aes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---- 测试辅助 ----

// decryptRequest 解出测试服务收到的 EAPI 请求，返回接口路径与参数。
func decryptRequest(t *testing.T, form url.Values) (string, map[string]any) {
	t.Helper()
	ct, err := hex.DecodeString(form.Get("params"))
	if err != nil {
		t.Fatalf("params 不是十六进制: %v", err)
	}
	block, err := aes.NewCipher(eapiKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		t.Fatalf("密文长度不对: %d", len(ct))
	}
	plain := make([]byte, len(ct))
	for i := 0; i < len(ct); i += aes.BlockSize {
		block.Decrypt(plain[i:i+aes.BlockSize], ct[i:i+aes.BlockSize])
	}
	if pad := int(plain[len(plain)-1]); pad > 0 && pad <= aes.BlockSize {
		plain = plain[:len(plain)-pad]
	}
	parts := strings.SplitN(string(plain), "-36cd479b6b5-", 3)
	if len(parts) != 3 {
		t.Fatalf("明文结构不对: %q", plain)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(parts[1]), &data); err != nil {
		t.Fatalf("参数不是 JSON: %v", err)
	}
	return parts[0], data
}

// seen 记录测试服务收到的一次请求。
type seen struct {
	path   string
	data   map[string]any
	cookie string
	ua     string
}

// recorder 收集测试服务收到的请求。
type recorder struct{ hits []seen }

// endpoint 返回一个记录请求并按 fix 回复的测试服务及其地址。
func endpoint(t *testing.T, rec *recorder, fix http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("解析表单失败: %v", err)
		}
		path, data := decryptRequest(t, r.PostForm)
		rec.hits = append(rec.hits, seen{path: path, data: data, cookie: r.Header.Get("Cookie"), ua: r.Header.Get("User-Agent")})
		fix(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func replyJSON(body string, setCookies ...string) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for _, c := range setCookies {
			w.Header().Add("Set-Cookie", c)
		}
		_, _ = w.Write([]byte(body))
	})
}

func replyStatus(status int, body string) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// newClient 以 handlers 依次作为接口入口创建客户端，loggedIn 为 true 时带有
// 登录凭据；第二个返回值用于查看测试服务收到的请求。
func newClient(t *testing.T, loggedIn bool, handlers ...http.HandlerFunc) (*Client, *recorder) {
	t.Helper()
	rec := &recorder{}
	c, err := NewClient(filepath.Join(t.TempDir(), "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	urls := make([]string, len(handlers))
	for i, h := range handlers {
		urls[i] = endpoint(t, rec, h)
	}
	c.SetEndpoints(urls)
	if loggedIn {
		c.absorb([]string{"MUSIC_U=token-1; Path=/; Max-Age=15552000"})
	}
	return c, rec
}

// headerOf 取请求头字段（JSON 解码后为 map[string]any）。
func headerOf(t *testing.T, s seen) map[string]any {
	t.Helper()
	h, ok := s.data["header"].(map[string]any)
	if !ok {
		t.Fatalf("请求缺少 header: %v", s.data)
	}
	return h
}

// ---- 传输层 ----

// 请求必须是完整的 PC 客户端档案；入口故障时顺延到下一个并保持使用。
func TestCallPCProfileAndFailover(t *testing.T) {
	ok := `{"code":200,"data":"ok"}`
	c, rec := newClient(t, true,
		replyStatus(http.StatusBadGateway, "<html><h1>502 Bad Gateway</h1></html>"), // 如已下线的网关入口
		replyJSON(ok),
		replyJSON(ok),
	)
	code, body, err := c.call("测试接口", "/api/test/echo", map[string]any{"k": "v"})
	if err != nil || code != 200 || string(body) != ok {
		t.Fatalf("应顺延到可用入口: code=%v err=%v body=%s", code, err, body)
	}
	if len(rec.hits) != 2 || rec.hits[0].path != "/api/test/echo" || rec.hits[1].path != "/api/test/echo" {
		t.Fatalf("应依次请求前两个入口: %+v", rec.hits)
	}

	first := rec.hits[0]
	h := headerOf(t, first)
	if first.ua != deviceUA {
		t.Errorf("User-Agent 应为 PC 客户端: %q", first.ua)
	}
	want := map[string]string{
		"os": "pc", "appver": "3.1.29.205117", "channel": "netease",
		"osver": "Microsoft-Windows-10-Professional-build-19045-64bit", "versioncode": "140",
		"resolution": "1920x1080", "MUSIC_U": "token-1", "deviceId": c.sess.DeviceID,
	}
	for k, v := range want {
		if got, _ := h[k].(string); got != v {
			t.Errorf("头字段 %s = %q，期望 %q", k, got, v)
		}
	}
	if len(c.sess.DeviceID) != 52 {
		t.Errorf("设备 ID 应为 52 位: %q", c.sess.DeviceID)
	}
	if first.data["k"] != "v" || first.data["e_r"] != false {
		t.Errorf("业务参数不对: %v", first.data)
	}
	// Cookie 串：凭据排最前，版本号等字段非空。
	if !strings.HasPrefix(first.cookie, "MUSIC_U=token-1; ") {
		t.Errorf("MUSIC_U 应排在 Cookie 最前: %q", first.cookie)
	}
	for _, kv := range []string{"appver=3.1.29.205117", "os=pc", "channel=netease", "deviceId=" + c.sess.DeviceID} {
		if !strings.Contains(first.cookie, "; "+kv+"; ") && !strings.HasSuffix(first.cookie, "; "+kv) {
			t.Errorf("Cookie 缺少 %q: %q", kv, first.cookie)
		}
	}

	// 成功后保持可用入口：再次调用不再碰第一个。
	if _, _, err := c.call("测试接口", "/api/test/echo", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if len(rec.hits) != 3 || rec.hits[2].cookie == "" {
		t.Fatalf("再次调用应直接走第二个入口: 请求数=%d", len(rec.hits))
	}
}

// 所有入口都不可用时返回传输错误（不是业务状态）。
func TestCallAllEndpointsFail(t *testing.T) {
	c, _ := newClient(t, true, replyStatus(502, "bad gateway"))
	_, _, err := c.call("测试接口", "/api/test/echo", nil)
	if err == nil || !strings.Contains(err.Error(), "测试接口（网络传输错误）") {
		t.Fatalf("应返回带接口名的传输错误: %v", err)
	}
	if IsStatusError(err) {
		t.Fatal("传输失败不应被当作业务状态")
	}
}

// 服务端下发的凭据并入会话并立即落盘；清除登录态的 Cookie 同步移除。
func TestAbsorbSessionCookies(t *testing.T) {
	c, _ := newClient(t, true, replyJSON(`{"code":200}`,
		"MUSIC_U=token-2; Max-Age=15552000; Path=/; Domain=.music.163.com",
		"NMTID=nmtid-1; Path=/",
		"__csrf=csrf-2; Max-Age=1296010; Path=/; Domain=.music.163.com",
		"MUSIC_A_T=whatever; Max-Age=1; Path=/api/feedback"))
	if _, _, err := c.call("刷新登录态", "/api/login/token/refresh", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewClient(c.path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.sess.MusicU != "token-2" || reloaded.sess.CSRF != "csrf-2" || reloaded.sess.NMTID != "nmtid-1" {
		t.Fatalf("新凭据应已落盘: %+v", reloaded.sess)
	}
	if reloaded.sess.IssuedAt.IsZero() || reloaded.sess.DeviceID != c.sess.DeviceID {
		t.Fatalf("应记录换发时间并保持设备 ID: %+v", reloaded.sess)
	}

	// 之后的请求携带新凭据。
	c2, rec := newClient(t, true, replyJSON(`{"code":200}`))
	c2.absorb([]string{"MUSIC_U=token-9; Path=/"})
	if _, _, err := c2.call("测试接口", "/api/test/echo", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rec.hits[0].cookie, "MUSIC_U=token-9; ") || headerOf(t, rec.hits[0])["MUSIC_U"] != "token-9" {
		t.Fatalf("请求应携带换发后的凭据: %q", rec.hits[0].cookie)
	}
	c2.absorb([]string{"MUSIC_U=; Max-Age=0; Path=/"})
	if c2.LoggedIn() {
		t.Fatal("服务端清除 MUSIC_U 后应为未登录")
	}
	if again, _ := NewClient(c2.path); again.LoggedIn() {
		t.Fatal("未登录状态应已落盘")
	}
}

// ---- 登录态 ----

func TestAccountInfo(t *testing.T) {
	// 本地没有凭据：不发请求，直接判为未登录。
	c, rec := newClient(t, false, replyJSON(`{"code":200}`))
	if acc, err := c.AccountInfo(); acc != nil || err != nil || len(rec.hits) != 0 {
		t.Fatalf("无凭据应直接返回未登录: acc=%v err=%v 请求数=%d", acc, err, len(rec.hits))
	}
	for _, tc := range []struct {
		name, body string
		account    *Account
	}{
		{"正常账号", `{"code":200,"account":{"id":7},"profile":{"nickname":"某人"}}`, &Account{ID: 7, Nickname: "某人"}},
		{"未登录", `{"code":200,"account":null,"profile":null}`, nil},
		{"匿名账号", `{"code":200,"account":{"id":1,"anonimous":true},"profile":{"nickname":"游客"}}`, nil},
	} {
		c, _ := newClient(t, true, replyJSON(tc.body))
		acc, err := c.AccountInfo()
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if (acc == nil) != (tc.account == nil) || acc != nil && *acc != *tc.account {
			t.Errorf("%s: 得到 %+v，期望 %+v", tc.name, acc, tc.account)
		}
	}
	c, _ = newClient(t, true, replyJSON(`{"code":301,"message":"需要登录"}`))
	if _, err := c.AccountInfo(); !IsStatusError(err) {
		t.Fatalf("业务状态码应构造成状态错误: %v", err)
	}
}

func TestQRLogin(t *testing.T) {
	c, rec := newClient(t, false, replyJSON(`{"code":200,"unikey":"KEY123"}`))
	qr, err := c.NewQRLogin()
	if err != nil {
		t.Fatal(err)
	}
	if qr.UniKey != "KEY123" || qr.URL != "https://music.163.com/login?codekey=KEY123" {
		t.Fatalf("二维码会话不对: %+v", qr)
	}
	if rec.hits[0].path != "/api/login/qrcode/unikey" || rec.hits[0].data["type"] != float64(3) {
		t.Fatalf("取 key 请求不对: %+v", rec.hits[0])
	}

	// 轮询：登录成功时凭据随响应下发并入会话。
	c, rec = newClient(t, false, replyJSON(`{"code":803,"message":"成功"}`,
		"MUSIC_U=token-qr; Max-Age=15552000; Path=/; Domain=.music.163.com"))
	code, err := c.CheckQRLogin("KEY123")
	if err != nil || code != QRCodeSuccess {
		t.Fatalf("应返回 803: code=%d err=%v", code, err)
	}
	if rec.hits[0].path != "/api/login/qrcode/client/login" || rec.hits[0].data["key"] != "KEY123" || rec.hits[0].data["type"] != float64(3) {
		t.Fatalf("轮询请求不对: %+v", rec.hits[0])
	}
	if !c.LoggedIn() || c.RenewDue() {
		t.Fatalf("登录成功后应已登录且一个间隔内无需续期: logged=%v due=%v", c.LoggedIn(), c.RenewDue())
	}

	// 等待/过期等状态码原样返回。
	for _, status := range []int{QRCodeWaiting, QRCodeScanned, QRCodeExpired} {
		c, _ = newClient(t, false, replyJSON(`{"code":`+strconv.Itoa(status)+`}`))
		if code, err := c.CheckQRLogin("K"); err != nil || code != status {
			t.Fatalf("状态码 %d 应原样返回: code=%d err=%v", status, code, err)
		}
	}
	// 不属于登录流程的状态码（如风控）是业务状态错误。
	c, _ = newClient(t, false, replyJSON(`{"code":8821,"message":"触发风控"}`))
	if _, err := c.CheckQRLogin("K"); !IsStatusError(err) || !strings.Contains(err.Error(), "触发风控") {
		t.Fatalf("未知状态码应返回状态错误并带出原因: %v", err)
	}
}

func TestRefreshLogin(t *testing.T) {
	// 服务端换发新凭据：算续期成功，落盘，一个间隔内不再检查。
	c, rec := newClient(t, true, replyJSON(`{"code":200,"code":200}`,
		"MUSIC_U=token-new; Max-Age=15552000; Path=/; Domain=.music.163.com"))
	age := func(c *Client) { // 把上次检查时间拨回两天前：模拟签发较久、该续期的凭据
		c.mu.Lock()
		c.sess.RenewCheckedAt = time.Now().Add(-2 * renewInterval)
		c.mu.Unlock()
	}
	age(c)
	if !c.RenewDue() {
		t.Fatal("距上次检查超过一个间隔，应到续期检查时间")
	}
	renewed, err := c.RefreshLogin()
	if err != nil || !renewed {
		t.Fatalf("应续期成功: renewed=%v err=%v", renewed, err)
	}
	if rec.hits[0].path != "/api/login/token/refresh" {
		t.Fatalf("续期请求不对: %+v", rec.hits[0])
	}
	if c.sess.MusicU != "token-new" || c.RenewDue() {
		t.Fatalf("应保存新凭据且一个间隔内不再续期: %+v", c.sess)
	}
	info := c.SessionInfo()
	if !strings.Contains(info, "凭据签发于0 小时前") || !strings.Contains(info, "上次续期检查于0 小时前") {
		t.Fatalf("时间概况不对: %q", info)
	}

	// 服务端不换发（凭据签发不久）：正常返回，不算续期，同样记为已检查。
	c, _ = newClient(t, true, replyJSON(`{"code":200}`))
	age(c)
	if renewed, err := c.RefreshLogin(); err != nil || renewed {
		t.Fatalf("未换发凭据不应算续期成功: renewed=%v err=%v", renewed, err)
	}
	if c.sess.MusicU != "token-1" || c.RenewDue() {
		t.Fatalf("凭据应保持不变且记为已检查: %+v", c.sess)
	}

	// 请求失败：不更新检查时间，留待下次再试。
	c, _ = newClient(t, true, replyStatus(502, "bad gateway"))
	age(c)
	if _, err := c.RefreshLogin(); err == nil {
		t.Fatal("入口故障应返回错误")
	}
	if !c.RenewDue() {
		t.Fatal("失败不应记为已检查")
	}
	c, _ = newClient(t, true, replyJSON(`{"code":400,"message":"错误"}`))
	age(c)
	if _, err := c.RefreshLogin(); !IsStatusError(err) || c.RenewDue() {
		t.Fatalf("服务端已答复的业务错误应记为已检查: %v", err)
	}
}

// ---- 业务接口 ----

const songJSON = `{"id":%d,"name":"歌%d","dt":%d,"ar":[{"id":1,"name":"甲"},{"id":2,"name":"乙"}],"al":{"id":9,"name":"专辑","picUrl":"http://p4.music.126.net/cover/%d.jpg"}}`

func TestPlaylistSongs(t *testing.T) {
	// 歌单详情只带第 1、3 首的详情：缺的用歌曲详情补齐，并按曲目 ID 顺序输出；
	// 第 4 首已下架（两处都没有），自然跳过。
	song := func(id int) string { return fmt.Sprintf(songJSON, id, id, 174000, id) }
	var playlistReqs, songReqs int
	var lastBatch any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, data := decryptRequest(t, mustForm(t, r))
		switch r.URL.Path {
		case "/eapi/v6/playlist/detail":
			playlistReqs++
			if data["n"] != float64(100000) {
				t.Errorf("歌单详情请求不对: %v", data)
			}
			tracks := song(1) + `,` + song(3)
			if playlistReqs > 1 {
				tracks += `,` + song(2) + `,` + song(4) // 第二次返回完整详情
			}
			_, _ = w.Write([]byte(`{"code":200,"playlist":{"trackIds":[{"id":1},{"id":2},{"id":3},{"id":4}],
				"tracks":[` + tracks + `]}}`))
		case "/eapi/v3/song/detail":
			songReqs++
			lastBatch = data["c"]
			_, _ = w.Write([]byte(`{"code":200,"songs":[` + song(2) + `]}`))
		default:
			t.Errorf("意外的请求路径: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c, _ := newClient(t, true)
	c.SetEndpoints([]string{srv.URL})

	got, err := c.PlaylistSongs(5167841076)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("应得到 3 首歌: %+v", got)
	}
	for i, s := range got {
		if s.ID != int64(i+1) || s.Name != "歌"+strconv.Itoa(i+1) {
			t.Errorf("第 %d 首不对: %+v", i, s)
		}
	}
	if first := got[0]; first.Artists != "甲/乙" || first.Album != "专辑" || first.Duration != 174*time.Second ||
		first.CoverURL != "https://p4.music.126.net/cover/1.jpg" {
		t.Errorf("歌曲字段不对: %+v", first)
	}
	if lastBatch != `[{"id":2},{"id":4}]` {
		t.Fatalf("应只为缺失的曲目批量取详情: %v", lastBatch)
	}

	// 第二次请求的仍是同一歌单：歌单详情会再请求一次，歌曲详情不会。
	if got, err := c.PlaylistSongs(1); err != nil || len(got) != 4 || playlistReqs != 2 || songReqs != 1 {
		t.Fatalf("详情完整时不该再取歌曲详情: err=%v 首数=%d 歌单详情=%d 歌曲详情=%d", err, len(got), playlistReqs, songReqs)
	}
}

// mustForm 解析并返回请求表单。
func mustForm(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	if err := r.ParseForm(); err != nil {
		t.Errorf("解析表单失败: %v", err)
	}
	return r.PostForm
}

func TestSongURL(t *testing.T) {
	// 未登录不发请求：匿名只能拿到试听音质。
	c, rec := newClient(t, false, replyJSON(`{"code":200}`))
	if _, err := c.SongURL(1, "hires"); !errors.Is(err, ErrNotLoggedIn) || len(rec.hits) != 0 {
		t.Fatalf("未登录应直接报错且不发请求: err=%v 请求数=%d", err, len(rec.hits))
	}
	body := `{"code":200,"data":[{"url":"https://m701.music.126.net/x.flac","level":"hires","size":40491562,"type":"flac","code":200}]}`
	c, rec = newClient(t, true, replyJSON(body))
	u, err := c.SongURL(3376569082, "hires")
	if err != nil {
		t.Fatal(err)
	}
	if u.URL != "https://m701.music.126.net/x.flac" || u.Level != "hires" || u.Size != 40491562 || u.Type != "flac" {
		t.Fatalf("播放地址不对: %+v", u)
	}
	if rec.hits[0].path != "/api/song/enhance/player/url/v1" || rec.hits[0].data["ids"] != "[3376569082]" || rec.hits[0].data["level"] != "hires" {
		t.Fatalf("播放地址请求不对: %+v", rec.hits[0])
	}
	// 没有可用地址（版权限制等）是明确的业务结果，不是状态错误。
	c, _ = newClient(t, true, replyJSON(`{"code":200,"data":[{"url":null,"level":"hires","code":404}]}`))
	if _, err := c.SongURL(1, "hires"); err == nil {
		t.Fatal("无可用地址应报错")
	}
	c, _ = newClient(t, true, replyJSON(`{"code":-460,"message":"检测到您的网络环境存在风险，请稍后再试"}`))
	_, err = c.SongURL(1, "hires")
	if !IsStatusError(err) || !strings.Contains(err.Error(), "网络环境存在风险") {
		t.Fatalf("风控应作为业务状态带出原因: %v", err)
	}
}

func TestLyricsAndPlaylists(t *testing.T) {
	c, rec := newClient(t, true, replyJSON(`{"code":200,"lrc":{"lyric":"[00:01.00]hello\n[00:02.00]world"},
		"tlyric":{"lyric":"[00:01.00]你好"}}`))
	lines, err := c.Lyrics(42, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0].Text != "hello 你好" || lines[1].Text != "world" {
		t.Fatalf("歌词解析不对: %+v", lines)
	}
	if rec.hits[0].path != "/api/song/lyric" || rec.hits[0].data["id"] != float64(42) {
		t.Fatalf("歌词请求不对: %+v", rec.hits[0])
	}

	c, rec = newClient(t, true, replyJSON(`{"code":200,"playlist":[
		{"id":10,"name":"自建","trackCount":3,"creator":{"userId":7}},
		{"id":11,"name":"收藏","trackCount":5,"creator":{"userId":99}}]}`))
	ps, err := c.UserPlaylists(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || !ps[0].Created || ps[1].Created || ps[1].TrackCount != 5 {
		t.Fatalf("歌单列表不对: %+v", ps)
	}
	if rec.hits[0].path != "/api/user/playlist" || rec.hits[0].data["uid"] != float64(7) {
		t.Fatalf("歌单列表请求不对: %+v", rec.hits[0])
	}
}

// ---- 会话文件 ----

func TestSessionFileLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	c, err := NewClient(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.LoggedIn() || len(c.sess.DeviceID) != 52 {
		t.Fatalf("新会话应为未登录并带设备 ID: %+v", c.sess)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("会话文件应存在且仅本人可读写: %v %v", fi, err)
	}
	again, err := NewClient(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.sess.DeviceID != c.sess.DeviceID {
		t.Fatal("设备 ID 应跨启动保持不变")
	}
	if raw, err := os.ReadFile(path); err != nil || strings.Contains(string(raw), "music_u") {
		t.Fatalf("未登录时不应写入空的凭据字段: %s %v", raw, err)
	}
}

// 旧版 Netscape 格式的 cookies 文件在首次启动时迁移：只取未过期的凭据，
// 迁移成功后删除旧文件。
func TestLegacyCookieMigration(t *testing.T) {
	dir := t.TempDir()
	future, past := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10), strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	legacy := strings.Join([]string{
		"# Netscape HTTP Cookie File",
		"#HttpOnly_ music.163.com\tFALSE\t/\tTRUE\t" + future + "\tMUSIC_U\tTOKEN-FROM-LEGACY",
		"music.163.com\tFALSE\t/\tTRUE\t" + past + "\t__csrf\tEXPIRED-CSRF",
		"music.163.com\tFALSE\t/\tFALSE\t253402300799\tNMTID\tsome_random_id_from_strategy",
		"music.163.com\tFALSE\t/api/feedback\tTRUE\t" + future + "\tMUSIC_A_T\tRISK-TOKEN",
		"\tmalformed line",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, legacyCookieFile), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(filepath.Join(dir, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.sess.MusicU != "TOKEN-FROM-LEGACY" {
		t.Fatalf("应迁移 MUSIC_U: %+v", c.sess)
	}
	if c.sess.CSRF != "" || c.sess.NMTID != "" {
		t.Fatalf("过期或无关的 Cookie 不应迁移: %+v", c.sess)
	}
	if c.sess.IssuedAt.IsZero() || !c.RenewDue() {
		t.Fatalf("迁移的凭据没有续期记录，应到检查时间: %+v", c.sess)
	}
	if _, err := os.Stat(filepath.Join(dir, legacyCookieFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("迁移成功后应删除旧文件: %v", err)
	}
	if again, err := NewClient(c.path); err != nil || again.sess.MusicU != "TOKEN-FROM-LEGACY" {
		t.Fatalf("迁移结果应已落盘: %v %+v", err, again.sess)
	}

	// 旧文件里没有登录凭据：保持原样，不迁移也不删除。
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, legacyCookieFile), []byte("#HttpOnly_ music.163.com\tFALSE\t/\tFALSE\t253402300799\tos\tpc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err = NewClient(filepath.Join(dir, "session.json"))
	if err != nil || c.LoggedIn() {
		t.Fatalf("无凭据的旧文件不应迁移: %v %+v", err, c.sess)
	}
	if _, err := os.Stat(filepath.Join(dir, legacyCookieFile)); err != nil {
		t.Fatalf("无凭据的旧文件不应被删除: %v", err)
	}
}

// 会话文件损坏时：原样备份、按未登录继续，不让损坏文件被静默覆盖。
func TestCorruptSessionBackedUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(path, []byte(`{"music_u": "broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(path)
	if err != nil || c.LoggedIn() {
		t.Fatalf("损坏文件应按未登录继续: %v %+v", err, c.sess)
	}
	if raw, err := os.ReadFile(path + ".bad"); err != nil || string(raw) != `{"music_u": "broken` {
		t.Fatalf("损坏文件应原样备份: %s %v", raw, err)
	}
}
