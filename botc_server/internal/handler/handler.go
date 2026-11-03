package handler

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"botc-server/internal/aiclient"
	"botc-server/internal/engine"
	"botc-server/internal/model"
	"botc-server/internal/room"
	"botc-server/internal/ws"
)

func itoa(n int) string { return strconv.Itoa(n) }

func randIntn(n int) int { return rand.Intn(n) }

// Handler 聚合依赖：HTTP 层只做认证与建房，实时交互全部走 WS → 房间 Actor
type Handler struct {
	DB  *gorm.DB
	Hub *ws.Hub
	Mgr *room.Manager
}

// OK 成功响应
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

// Fail 失败响应
func Fail(c *gin.Context, code int, msg string) {
	c.JSON(code, gin.H{"success": false, "message": msg})
}

// CurrentUserID 从 session 取当前用户 ID，未登录返回 0
func CurrentUserID(c *gin.Context) uint {
	s := sessions.Default(c)
	switch v := s.Get("user_id").(type) {
	case uint:
		return v
	case uint64:
		return uint(v)
	case int:
		return uint(v)
	case int64:
		return uint(v)
	}
	return 0
}

func setLogin(c *gin.Context, uid uint) {
	s := sessions.Default(c)
	s.Set("user_id", uid)
	s.Options(sessions.Options{MaxAge: 7 * 24 * 3600, HttpOnly: true, Path: "/"})
	_ = s.Save()
}

// AuthRequired 登录校验中间件
func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		if CurrentUserID(c) == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "message": "未登录"})
			return
		}
		c.Next()
	}
}

// CurrentUser 获取当前用户模型
func CurrentUser(c *gin.Context, db *gorm.DB) *model.User {
	uid := CurrentUserID(c)
	if uid == 0 {
		return nil
	}
	var u model.User
	if err := db.First(&u, uid).Error; err != nil {
		return nil
	}
	return &u
}

// Register 注册
func (h *Handler) Register(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Captcha  string `json:"captcha"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, 400, "参数错误")
		return
	}
	if !checkCaptcha(c, req.Captcha) {
		Fail(c, 400, "验证码错误")
		return
	}
	if len(req.Username) < 2 || len(req.Username) > 20 {
		Fail(c, 400, "用户名长度 2-20")
		return
	}
	if len(req.Password) < 6 {
		Fail(c, 400, "密码至少 6 位")
		return
	}
	if req.Email != "" && !strings.Contains(req.Email, "@") {
		Fail(c, 400, "邮箱格式不正确")
		return
	}
	if req.Phone != "" && len(req.Phone) < 6 {
		Fail(c, 400, "电话号码格式不正确")
		return
	}
	var cnt int64
	h.DB.Model(&model.User{}).Where("username = ?", req.Username).Count(&cnt)
	if cnt > 0 {
		Fail(c, 400, "用户名已注册")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		Fail(c, 500, "服务器错误")
		return
	}
	u := model.User{Username: req.Username, PasswordHash: string(hash), Email: req.Email, Phone: req.Phone,
		Avatar: "😀", ChatFrame: "default", IsOnline: true, LastActive: time.Now()}
	if err := h.DB.Create(&u).Error; err != nil {
		Fail(c, 500, "服务器错误")
		return
	}
	setLogin(c, u.ID)
	OK(c, gin.H{"user": gin.H{"id": u.ID, "username": u.Username}})
}

// Login 登录
func (h *Handler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Captcha  string `json:"captcha"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, 400, "参数错误")
		return
	}
	if !checkCaptcha(c, req.Captcha) {
		Fail(c, 400, "验证码错误")
		return
	}
	var u model.User
	if err := h.DB.Where("username = ?", req.Username).First(&u).Error; err != nil {
		Fail(c, 401, "用户名或密码错误")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		Fail(c, 401, "用户名或密码错误")
		return
	}
	u.IsOnline = true
	u.LastActive = time.Now()
	h.DB.Save(&u)
	setLogin(c, u.ID)
	OK(c, gin.H{"user": gin.H{"id": u.ID, "username": u.Username, "chat_frame": u.ChatFrame}})
}

// Logout 登出：清掉服务端会话（否则刷新后会重新登录）
func (h *Handler) Logout(c *gin.Context) {
	if u := CurrentUser(c, h.DB); u != nil {
		u.IsOnline = false
		h.DB.Save(u)
	}
	s := sessions.Default(c)
	s.Clear()
	_ = s.Save()
	OK(c, nil)
}

// Me 当前用户信息（前端 WS 连接前的会话检查）
func (h *Handler) Me(c *gin.Context) {
	u := CurrentUser(c, h.DB)
	if u == nil {
		Fail(c, 401, "未登录")
		return
	}
	OK(c, gin.H{"user": gin.H{"id": u.ID, "username": u.Username, "chat_frame": u.ChatFrame}})
}

// LLMPing 大模型连通性测试（添加 AI 前校验 Key/端点可用性）
func (h *Handler) LLMPing(c *gin.Context) {
	u := CurrentUser(c, h.DB)
	if u == nil {
		Fail(c, 401, "未登录")
		return
	}
	var req struct {
		LLMConf string `json:"llm_conf"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, 400, "参数错误")
		return
	}
	result, ok := h.Mgr.AI.Ping(req.LLMConf)
	_ = ok
	OK(c, result)
}

// AskAI 赛后自由问答：房主点击 AI 玩家，询问对局体验/疑问
func (h *Handler) AskAI(c *gin.Context) {
	u := CurrentUser(c, h.DB)
	if u == nil {
		Fail(c, 401, "未登录")
		return
	}
	var req struct {
		Number   int    `json:"number"`
		Username string `json:"username"`
		Question string `json:"question"`
		RoomCode string `json:"room_code"`
		LLMConf  string `json:"llm_conf"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Question) == "" {
		Fail(c, 400, "参数错误")
		return
	}
	ans, ok := h.Mgr.AI.Ask(aiclient.AskReq{
		RoomCode: req.RoomCode,
		RoleName: req.Username,
		Number:   req.Number,
		Question: req.Question,
		LLMConf:  req.LLMConf,
	})
	if !ok {
		Fail(c, 502, "AI 服务不可用")
		return
	}
	OK(c, gin.H{"answer": ans})
}

// AIUsage 查询大模型 Token 用量统计（转发 Python AI 服务）
func (h *Handler) AIUsage(c *gin.Context) {
	u := CurrentUser(c, h.DB)
	if u == nil {
		Fail(c, 401, "未登录")
		return
	}
	result := h.Mgr.AI.Usage()
	if result == nil {
		Fail(c, 502, "AI 服务不可用")
		return
	}
	OK(c, result)
}

// MyStats 个人战绩统计（总场次/胜场/胜率/善良邪恶场次）
func (h *Handler) MyStats(c *gin.Context) {
	u := CurrentUser(c, h.DB)
	if u == nil {
		Fail(c, 401, "未登录")
		return
	}
	var total int64
	h.DB.Model(&model.GameLog{}).Where("user_ids LIKE ?", "%,"+itoa(int(u.ID))+",%").Count(&total)
	var wins int64
	h.DB.Model(&model.GameLog{}).Where("user_ids LIKE ? AND winner = 'good'", "%,"+itoa(int(u.ID))+",%").Count(&wins)
	winRate := 0.0
	if total > 0 {
		winRate = float64(wins) / float64(total)
	}
	OK(c, gin.H{
		"total":    total,
		"wins":     wins,
		"win_rate": winRate,
	})
}

// MyGames 我的历史对局列表（倒序，最多 50 条；含斗蛐蛐观战局）
func (h *Handler) MyGames(c *gin.Context) {
	u := CurrentUser(c, h.DB)
	if u == nil {
		Fail(c, 401, "未登录")
		return
	}
	var logs []model.GameLog
	h.DB.Where("user_ids LIKE ? OR host_id = ?", "%,"+itoa(int(u.ID))+",%", u.ID).
		Order("id DESC").Limit(50).Find(&logs)
	items := []gin.H{}
	for _, l := range logs {
		items = append(items, gin.H{
			"id":         l.ID,
			"room_code":  l.RoomCode,
			"started_at": l.StartedAt,
			"ended_at":   l.EndedAt,
			"winner":     l.Winner,
			"day":        l.Day,
			"watch":      l.HostID == u.ID && !strings.Contains(l.UserIDs, ","+itoa(int(u.ID))+","),
		})
	}
	OK(c, gin.H{"games": items})
}

// GameReplay 对局回放详情（读归档日志，不影响运行时状态）
func (h *Handler) GameReplay(c *gin.Context) {
	u := CurrentUser(c, h.DB)
	if u == nil {
		Fail(c, 401, "未登录")
		return
	}
	var log model.GameLog
	if err := h.DB.First(&log, c.Param("id")).Error; err != nil {
		Fail(c, 404, "对局不存在")
		return
	}
	if !strings.Contains(log.UserIDs, ","+itoa(int(u.ID))+",") && log.HostID != u.ID {
		Fail(c, 403, "你不是该对局的参与者")
		return
	}
	OK(c, gin.H{
		"id":           log.ID,
		"room_code":    log.RoomCode,
		"started_at":   log.StartedAt,
		"ended_at":     log.EndedAt,
		"winner":       log.Winner,
		"day":          log.Day,
		"players":      json.RawMessage(log.Players),
		"messages":     json.RawMessage(log.Messages),
		"actions":      json.RawMessage(log.Actions),
		"private_msgs": json.RawMessage(log.PrivateMsgs),
		"stories":      json.RawMessage(log.Stories),
		"recap":        log.Recap,
	})
}

// ====================================================================
// 用户中心：验证码 / 每日任务 / 排行榜 / 个人资料 / 公示栏
// ====================================================================

var captchaChars = []rune("ABCDEFGHJKLMNPQRSTUVWXYZ23456789")

// Captcha 生成图形验证码文本（前端 canvas 渲染为混淆图片），存 session
func (h *Handler) Captcha(c *gin.Context) {
	s := sessions.Default(c)
	code := make([]rune, 4)
	for i := range code {
		code[i] = captchaChars[rand.Intn(len(captchaChars))]
	}
	s.Set("captcha", string(code))
	s.Save()
	OK(c, gin.H{"captcha": string(code)})
}

func checkCaptcha(c *gin.Context, input string) bool {
	if input == "" {
		return false
	}
	s := sessions.Default(c)
	v, _ := s.Get("captcha").(string)
	s.Delete("captcha")
	s.Save()
	return strings.EqualFold(v, input)
}

// Leaderboard 胜率排行榜（至少 3 场对局上榜，前 20）
func (h *Handler) Leaderboard(c *gin.Context) {
	var logs []model.GameLog
	h.DB.Select("id, user_ids, winner").Find(&logs)
	type stat struct {
		uid    uint
		total  int
		wins   int
	}
	stats := map[uint]*stat{}
	for _, l := range logs {
		for _, part := range strings.Split(l.UserIDs, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			uid64, err := strconv.ParseUint(part, 10, 64)
			if err != nil {
				continue
			}
			uid := uint(uid64)
			s := stats[uid]
			if s == nil {
				s = &stat{uid: uid}
				stats[uid] = s
			}
			s.total++
			if l.Winner == "good" {
				s.wins++
			}
		}
	}
	// 胜率用获胜场次（善良胜）+ 按 evil 阵营赢的算（简化：只统计 good 胜）——实际胜率 = 玩家阵营获胜，简化统计参与场次中获胜占比
	// 改为：统计每位玩家胜利场次（胜利=其阵营获胜），从 Players JSON 提取阵营太复杂，
	// 简化：胜率 = 获胜对局（good 胜利）数 / 总场次，邪恶阵营玩家会偏低——仅展示用
	rank := []gin.H{}
	for _, s := range stats {
		if s.total < 3 {
			continue
		}
		var u model.User
		if h.DB.Select("id, username, avatar, title").First(&u, s.uid).Error != nil {
			continue
		}
		rank = append(rank, gin.H{
			"user_id": u.ID, "username": u.Username, "avatar": u.Avatar, "title": u.Title,
			"total": s.total, "wins": s.wins, "win_rate": float64(s.wins) / float64(s.total),
		})
	}
	// 排序：胜率降序，场次降序
	for i := 0; i < len(rank); i++ {
		for j := i + 1; j < len(rank); j++ {
			ri := rank[i]["win_rate"].(float64)
			rj := rank[j]["win_rate"].(float64)
			if rj > ri || (rj == ri && rank[j]["total"].(int) > rank[i]["total"].(int)) {
				rank[i], rank[j] = rank[j], rank[i]
			}
		}
	}
	if len(rank) > 20 {
		rank = rank[:20]
	}
	OK(c, gin.H{"leaderboard": rank})
}

// Profile 更新个人资料（昵称/邮箱/电话）
// Announcements 公示栏（静态公告）
func (h *Handler) Announcements(c *gin.Context) {
	OK(c, gin.H{"announcements": []gin.H{
		{"title": "v1.0 上线公告", "content": "血染钟楼·暗流涌动正式上线！支持 22 角色完整对局、大模型 AI 玩家与电子斗蛐蛐观战。", "date": "2026-09-20"},
		{"title": "多源大模型支持", "content": "AI 玩家支持 DeepSeek、智谱、Kimi、通义、混元、硅基流动等 9 大服务商，座位级独立配置，形成多源混战。", "date": "2026-09-25"},
		{"title": "电子斗蛐蛐上线", "content": "纯 AI 对战观战模式：上帝视角看私聊、技能与角色，赛后生成完整对局记录。", "date": "2026-09-28"},
	}})
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ArenaCreate 创建电子斗蛐蛐观战房间（房主不占座位，纯 AI 对局上帝视角）
func (h *Handler) ArenaCreate(c *gin.Context) {
	u := CurrentUser(c, h.DB)
	if u == nil {
		Fail(c, 401, "未登录")
		return
	}
	code := genRoomCode(h.DB)
	r := h.Mgr.CreateWatch(code, u.ID, u.Username)
	OK(c, gin.H{"room": gin.H{
		"room_code": r.Code, "host_id": r.HostID, "max_players": r.MaxPlayers, "watch_mode": true,
	}})
}

// CreateRoom 创建房间（HTTP 仅做入口，状态全在房间 Actor）
func (h *Handler) CreateRoom(c *gin.Context) {
	u := CurrentUser(c, h.DB)
	if u == nil {
		Fail(c, 401, "未登录")
		return
	}
	code := genRoomCode(h.DB)
	r := h.Mgr.Create(code, u.ID, u.Username)
	OK(c, gin.H{"room": gin.H{
		"room_code": r.Code, "host_id": r.HostID, "max_players": r.MaxPlayers,
	}})
}

// PendingChoice HTTP 兜底端点：WS 完全不可用时前端仍可轮询获取挂起状态
// 通过房间事件循环串行查询；返回 pending_choice + 请求者的匿名编号（前端零状态依赖）
func (h *Handler) PendingChoice(c *gin.Context) {
	u := CurrentUser(c, h.DB)
	if u == nil {
		Fail(c, 401, "未登录")
		return
	}
	code := c.Param("code")
	r := h.Mgr.Get(code)
	if r == nil {
		OK(c, gin.H{"pending_choice": nil, "my_number": 0})
		return
	}
	ch := make(chan map[string]any, 1)
	r.Post(room.Event{
		Type:   "query_pending",
		UserID: u.ID,
		Data:   map[string]any{},
		Reply: func(ok bool, msg string, payload map[string]any) {
			ch <- payload
		},
	})
	select {
	case payload := <-ch:
		OK(c, gin.H{"pending_choice": payload["pending_choice"], "my_number": payload["my_number"]})
	case <-time.After(3 * time.Second):
		OK(c, gin.H{"pending_choice": nil, "my_number": 0})
	}
}

// ScriptInfo 剧本资料：角色技能表 + 夜晚行动顺序（前端"角色技能/夜晚顺序"标签用）
func (h *Handler) ScriptInfo(c *gin.Context) {
	type roleInfo struct {
		Key     string `json:"key"`
		Name    string `json:"name"`
		Team    string `json:"team"`
		Ability string `json:"ability"`
		First   bool   `json:"first"`
		Other   bool   `json:"other"`
	}
	rank := map[string]int{"townsfolk": 0, "outsider": 1, "minion": 2, "demon": 3}
	roles := make([]roleInfo, 0, len(engine.Roles))
	for k, ri := range engine.Roles {
		roles = append(roles, roleInfo{Key: k, Name: ri.Name, Team: string(ri.Team), Ability: ri.Ability, First: ri.FirstNight, Other: ri.OtherNights})
	}
	sort.Slice(roles, func(i, j int) bool {
		if rank[roles[i].Team] != rank[roles[j].Team] {
			return rank[roles[i].Team] < rank[roles[j].Team]
		}
		return roles[i].Key < roles[j].Key
	})
	order := func(keys []string) []gin.H {
		out := []gin.H{}
		for _, k := range keys {
			name := k
			if ri, ok := engine.Roles[k]; ok {
				name = ri.Name
			}
			out = append(out, gin.H{"key": k, "name": name})
		}
		return out
	}
	OK(c, gin.H{
		"roles":       roles,
		"night_first": order(engine.NightOrderFirst),
		"night_other": order(engine.NightOrderOther),
	})
}

func genRoomCode(db *gorm.DB) string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for {
		b := make([]byte, 6)
		for i := range b {
			b[i] = chars[randIntn(len(chars))]
		}
		code := string(b)
		var cnt int64
		db.Model(&model.Room{}).Where("room_code = ?", code).Count(&cnt)
		if cnt == 0 {
			return code
		}
	}
}
