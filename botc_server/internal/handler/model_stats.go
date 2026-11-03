package handler

import (
	"encoding/json"
	"math"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"botc-server/internal/model"
)

// ModelStats 全服模型战绩（多维度评估）
// 数据来源：所有 game_logs 的 players（模型/阵营/存活/胜负）+ messages（发言文本）
// 维度：胜负、存活、信息量、逻辑链、角色扮演、语言规范、互动性、跨局稳定性
func (h *Handler) ModelStats(c *gin.Context) {
	var logs []model.GameLog
	h.DB.Select("id, winner, players, messages").Find(&logs)

	type agg struct {
		games, wins           int
		goodGames, goodWins   int
		evilGames, evilWins   int
		survived              int
		msgs, chars           int
		info, logic           int
		role, inter, norm     int
		bad, outlier, dups    int
		perGameNorm           []float64
	}
	m := map[string]*agg{}
	get := func(name string) *agg {
		a := m[name]
		if a == nil {
			a = &agg{}
			m[name] = a
		}
		return a
	}

	logicWords := []string{"因为", "所以", "因此", "如果", "但是", "矛盾", "说明", "推理", "逻辑", "证据", "推断", "假设", "否则", "意味着"}
	roleWords := []string{"我是", "我的身份", "技能", "查验", "查了", "占卜", "守卫", "下毒", "恶魔", "爪牙", "好人", "坏人", "村民", "外来者", "提名", "怀疑", "相信", "声称"}
	badWords := []string{"狼人", "预言家", "女巫", "猎人", "警长", "悍跳", "查杀", "金水", "银水", "自刀", "刀人", "狼坑"}
	containAny := func(s string, words []string) bool {
		for _, w := range words {
			if strings.Contains(s, w) {
				return true
			}
		}
		return false
	}
	normKey := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			if r == ' ' || r == '\n' || r == '\t' || r == '，' || r == '。' || r == '！' || r == '？' || r == ',' || r == '.' {
				continue
			}
			b.WriteRune(r)
		}
		return b.String()
	}

	for _, l := range logs {
		if l.Winner != "good" && l.Winner != "evil" {
			continue
		}
		var players map[string]struct {
			IsAI    bool   `json:"is_ai"`
			AIModel string `json:"ai_model"`
			Team    string `json:"team"`
			Alive   bool   `json:"alive"`
		}
		if json.Unmarshal([]byte(l.Players), &players) != nil {
			continue
		}
		seen := map[string]bool{}
		for _, p := range players {
			if !p.IsAI || p.AIModel == "" || seen[p.AIModel] {
				continue
			}
			seen[p.AIModel] = true
			a := get(p.AIModel)
			a.games++
			if p.Alive {
				a.survived++
			}
			isGood := p.Team == "townsfolk" || p.Team == "outsider"
			if isGood {
				a.goodGames++
			} else {
				a.evilGames++
			}
			won := (l.Winner == "good" && isGood) || (l.Winner == "evil" && !isGood)
			if won {
				a.wins++
				if isGood {
					a.goodWins++
				} else {
					a.evilWins++
				}
			}
		}

		var msgs []struct {
			Number  int    `json:"number"`
			Content string `json:"content"`
			AIModel string `json:"ai_model"`
		}
		if json.Unmarshal([]byte(l.Messages), &msgs) != nil {
			continue
		}
		// 本局每个模型的规范发言比例（用于跨局稳定性）
		type pg struct{ norm, total int }
		perGame := map[string]*pg{}
		dupSeen := map[string]map[string]bool{}
		for _, msg := range msgs {
			if msg.AIModel == "" || msg.Number == 0 || strings.TrimSpace(msg.Content) == "" {
				continue
			}
			a := get(msg.AIModel)
			text := strings.TrimSpace(msg.Content)
			runes := len([]rune(text))
			a.msgs++
			a.chars += runes
			if strings.ContainsAny(text, "0123456789") || strings.Contains(text, "#") || strings.Contains(text, "号") {
				a.info++
			}
			if containAny(text, logicWords) {
				a.logic++
			}
			if containAny(text, roleWords) {
				a.role++
			}
			if strings.Contains(text, "@") || strings.Contains(text, "#") || strings.Contains(text, "号") {
				a.inter++
			}
			isBad := containAny(text, badWords)
			if isBad {
				a.bad++
			}
			isOutlier := runes < 8 || runes > 260
			if isOutlier {
				a.outlier++
			}
			// 复读检测（同一局内同一模型重复同一句话）
			if dupSeen[msg.AIModel] == nil {
				dupSeen[msg.AIModel] = map[string]bool{}
			}
			key := normKey(text)
			if dupSeen[msg.AIModel][key] {
				a.dups++
			} else {
				dupSeen[msg.AIModel][key] = true
			}
			if perGame[msg.AIModel] == nil {
				perGame[msg.AIModel] = &pg{}
			}
			perGame[msg.AIModel].total++
			if runes >= 15 && runes <= 220 && !isBad && !isOutlier {
				a.norm++
				perGame[msg.AIModel].norm++
			}
		}
		for name, g := range perGame {
			if g.total > 0 {
				get(name).perGameNorm = append(get(name).perGameNorm, float64(g.norm)/float64(g.total))
			}
		}
	}

	clamp := func(v float64) float64 { return math.Max(0, math.Min(1, v)) }
	ratio := func(part, total int) float64 {
		if total == 0 {
			return 0
		}
		return float64(part) / float64(total)
	}
	pct := func(v float64) int { return int(math.Round(v * 100)) }

	out := []gin.H{}
	for name, a := range m {
		// 1) 胜负分：小样本向 50% 收缩（贝叶斯平滑）
		winScore := 50.0
		if a.games > 0 {
			winScore = (float64(a.wins) + 2.5) / (float64(a.games) + 5) * 100
		}
		// 2) 存活分
		surviveScore := 50.0
		if a.games > 0 {
			surviveScore = float64(a.survived) / float64(a.games) * 100
		}
		// 3~7) 文本维度
		infoScore, logicScore, roleScore, normScore, interScore := 50.0, 50.0, 50.0, 50.0, 50.0
		stableScore := 70.0
		if a.msgs > 0 {
			infoScore = math.Min(1, ratio(a.info, a.msgs)/0.35) * 100
			logicScore = math.Min(1, ratio(a.logic, a.msgs)/0.45) * 100
			roleScore = math.Min(1, ratio(a.role, a.msgs)/0.30) * 100
			interScore = math.Min(1, ratio(a.inter, a.msgs)/0.25) * 100
			normScore = clamp(ratio(a.norm, a.msgs)-ratio(a.bad, a.msgs)*1.2-ratio(a.outlier, a.msgs)*0.6-ratio(a.dups, a.msgs)*0.5) * 100
		}
		// 8) 跨局稳定性：各局规范率的标准差越小越稳定
		if len(a.perGameNorm) >= 2 {
			mean := 0.0
			for _, v := range a.perGameNorm {
				mean += v
			}
			mean /= float64(len(a.perGameNorm))
			variance := 0.0
			for _, v := range a.perGameNorm {
				variance += (v - mean) * (v - mean)
			}
			variance /= float64(len(a.perGameNorm))
			std := math.Sqrt(variance)
			stableScore = math.Max(0, math.Min(100, 100-std*180))
		}
		// 9) 综合：多视角加权
		overall := 0.30*winScore + 0.20*((infoScore+logicScore)/2) + 0.15*roleScore +
			0.15*normScore + 0.10*stableScore + 0.10*interScore

		goodWR, evilWR := 0.0, 0.0
		if a.goodGames > 0 {
			goodWR = float64(a.goodWins) / float64(a.goodGames)
		}
		if a.evilGames > 0 {
			evilWR = float64(a.evilWins) / float64(a.evilGames)
		}
		out = append(out, gin.H{
			"model": name, "games": a.games, "wins": a.wins, "messages": a.msgs,
			"win_rate": float64(a.wins) / math.Max(1, float64(a.games)),
			"survive_rate": surviveScore / 100,
			"good_games": a.goodGames, "good_wr": goodWR,
			"evil_games": a.evilGames, "evil_wr": evilWR,
			"win_score": pct(winScore / 100),
			"info": pct(infoScore / 100), "logic": pct(logicScore / 100),
			"roleplay": pct(roleScore / 100), "norm": pct(normScore / 100),
			"stable": pct(stableScore / 100), "interact": pct(interScore / 100),
			"overall": pct(overall / 100),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		oi, _ := out[i]["overall"].(int)
		oj, _ := out[j]["overall"].(int)
		if oi != oj {
			return oi > oj
		}
		gi, _ := out[i]["games"].(int)
		gj, _ := out[j]["games"].(int)
		if gi != gj {
			return gi > gj
		}
		return out[i]["model"].(string) < out[j]["model"].(string)
	})
	OK(c, gin.H{"models": out})
}
