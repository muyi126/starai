package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/starai/api/internal/service"
	"github.com/starai/api/internal/util"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
	"github.com/volcengine/volcengine-go-sdk/volcengine/session"
	"github.com/volcengine/volcengine-go-sdk/volcengine/universal"
)

type seedancePortraitSession struct {
	UserID      int64  `json:"user_id"`
	BytedToken  string `json:"byted_token"`
	ProjectName string `json:"project_name"`
}

type seedanceCaptureTransport struct {
	base   http.RoundTripper
	status int
	body   []byte
}

func (t *seedanceCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil || resp.StatusCode < http.StatusMultipleChoices {
		return resp, err
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if readErr != nil {
		return resp, err
	}
	t.status, t.body = resp.StatusCode, body
	resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), resp.Body))
	return resp, err
}

func seedanceGatewayError(status int, body []byte, fallback error) error {
	var payload interface{}
	if json.Unmarshal(body, &payload) == nil {
		if message := seedanceErrorField(payload, "message", "detail"); message != "" {
			if code := seedanceErrorField(payload, "code"); code != "" && !strings.EqualFold(code, message) {
				return fmt.Errorf("网关 HTTP %d：%s：%s", status, code, message)
			}
			return fmt.Errorf("网关 HTTP %d：%s", status, message)
		}
	}
	plain := strings.TrimSpace(string(body))
	if len(plain) > 500 {
		plain = plain[:500]
	}
	if plain != "" {
		return fmt.Errorf("网关 HTTP %d：%s", status, plain)
	}
	return fallback
}

func seedanceErrorField(value interface{}, names ...string) string {
	switch value := value.(type) {
	case map[string]interface{}:
		for key, item := range value {
			for _, name := range names {
				if strings.EqualFold(key, name) {
					if text, ok := item.(string); ok {
						return strings.TrimSpace(text)
					}
				}
			}
		}
		for _, item := range value {
			if found := seedanceErrorField(item, names...); found != "" {
				return found
			}
		}
	case []interface{}:
		for _, item := range value {
			if found := seedanceErrorField(item, names...); found != "" {
				return found
			}
		}
	}
	return ""
}

func (h *Handler) seedancePortraitConfig(c *gin.Context) (service.SeedancePortraitConfig, bool) {
	cfg, err := h.admin.GetSeedancePortraitConfig(c.Request.Context())
	if err != nil {
		util.InternalError(c, err.Error())
		return cfg, false
	}
	if !cfg.Enabled {
		util.BadRequest(c, "管理员尚未启用 Seedance 真人素材")
		return cfg, false
	}
	if strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		util.BadRequest(c, "管理员尚未配置火山方舟 Access Key / Secret Key")
		return cfg, false
	}
	return cfg, true
}

func callSeedanceArk(cfg service.SeedancePortraitConfig, action string, body map[string]interface{}) (map[string]interface{}, error) {
	transport := &seedanceCaptureTransport{base: http.DefaultTransport}
	sdkConfig := volcengine.NewConfig().
		WithCredentials(credentials.NewStaticCredentials(cfg.AccessKey, cfg.SecretKey, "")).
		WithRegion("cn-beijing").
		WithHTTPClient(&http.Client{Timeout: 30 * time.Second, Transport: transport})
	sdkConfig.Endpoint = volcengine.String(strings.TrimRight(cfg.GatewayURL, "/"))
	sdkConfig.DisableRestProtocolURICleaning = volcengine.Bool(true)
	sess, err := session.NewSession(sdkConfig)
	if err != nil {
		return nil, err
	}
	resp, err := universal.New(sess).DoCall(universal.RequestUniversal{
		ServiceName: "ark", Action: action, Version: "2024-01-01",
		HttpMethod: universal.POST, ContentType: universal.ApplicationJSON,
	}, &body)
	if err != nil {
		if transport.status != 0 {
			return nil, seedanceGatewayError(transport.status, transport.body, err)
		}
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("火山方舟返回空响应")
	}
	return seedanceResult(*resp), nil
}

func seedanceResult(resp map[string]interface{}) map[string]interface{} {
	for key, value := range resp {
		if strings.EqualFold(key, "result") {
			if result, ok := value.(map[string]interface{}); ok {
				return result
			}
		}
	}
	return resp
}

func seedanceValue(obj map[string]interface{}, key string) interface{} {
	for name, value := range obj {
		if strings.EqualFold(name, key) {
			return value
		}
	}
	return nil
}

func seedanceString(obj map[string]interface{}, key string) string {
	value := seedanceValue(obj, key)
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func (h *Handler) CreateSeedancePortraitSession(c *gin.Context) {
	cfg, ok := h.seedancePortraitConfig(c)
	if !ok {
		return
	}
	if cfg.SiteBaseURL == "" {
		util.BadRequest(c, "请先在后台填写前台站点地址 site_base_url")
		return
	}
	result, err := callSeedanceArk(cfg, "CreateVisualValidateSession", map[string]interface{}{
		"CallbackURL": cfg.SiteBaseURL + "/app", "ProjectName": cfg.ProjectName,
	})
	if err != nil {
		util.BadRequest(c, "创建真人认证失败："+err.Error())
		return
	}
	bytedToken := seedanceString(result, "BytedToken")
	h5Link := seedanceString(result, "H5Link")
	if bytedToken == "" || h5Link == "" {
		util.BadRequest(c, "火山方舟未返回 BytedToken 或 H5Link")
		return
	}
	sessionID := uuid.NewString()
	raw, _ := json.Marshal(seedancePortraitSession{UserID: c.GetInt64("user_id"), BytedToken: bytedToken, ProjectName: cfg.ProjectName})
	if h.cache == nil || h.cache.SetTemp(c.Request.Context(), "seedance:portrait:session:"+sessionID, string(raw), 30*time.Minute) != nil {
		util.InternalError(c, "保存真人认证会话失败")
		return
	}
	util.Created(c, map[string]interface{}{"session_id": sessionID, "h5_link": h5Link, "expires_in": 1800})
}

func (h *Handler) CompleteSeedancePortraitSession(c *gin.Context) {
	cfg, ok := h.seedancePortraitConfig(c)
	if !ok {
		return
	}
	sessionID := strings.TrimSpace(c.Param("id"))
	raw, found := h.cache.GetTemp(c.Request.Context(), "seedance:portrait:session:"+sessionID)
	if !found {
		util.BadRequest(c, "真人认证会话已过期，请重新认证")
		return
	}
	var pending seedancePortraitSession
	if json.Unmarshal([]byte(raw), &pending) != nil || pending.UserID != c.GetInt64("user_id") || pending.ProjectName != cfg.ProjectName {
		util.Forbidden(c, "无权访问该真人认证会话")
		return
	}
	result, err := callSeedanceArk(cfg, "GetVisualValidateResult", map[string]interface{}{
		"BytedToken": pending.BytedToken, "ProjectName": pending.ProjectName,
	})
	if err != nil {
		util.BadRequest(c, "查询真人认证结果失败："+err.Error())
		return
	}
	groupID := seedanceString(result, "GroupId")
	if groupID == "" {
		util.BadRequest(c, "尚未取得真人素材组，请完成 H5 活体认证后重试")
		return
	}
	if err := h.assets.SaveSeedancePortraitGroup(c.Request.Context(), pending.UserID, groupID, pending.ProjectName); err != nil {
		util.InternalError(c, "保存真人素材组失败")
		return
	}
	h.cache.DelTemp(c.Request.Context(), "seedance:portrait:session:"+sessionID)
	util.OK(c, map[string]interface{}{"group_id": groupID, "project_name": pending.ProjectName})
}

func (h *Handler) ListSeedancePortraitAssets(c *gin.Context) {
	cfg, ok := h.seedancePortraitConfig(c)
	if !ok {
		return
	}
	groups, err := h.assets.ListSeedancePortraitGroups(c.Request.Context(), c.GetInt64("user_id"))
	if err != nil {
		util.InternalError(c, "读取真人素材组失败")
		return
	}
	groupIDs := []string{}
	visibleGroups := []service.SeedancePortraitGroup{}
	for _, group := range groups {
		if group.ProjectName == cfg.ProjectName {
			groupIDs = append(groupIDs, group.GroupID)
			visibleGroups = append(visibleGroups, group)
		}
	}
	if len(groupIDs) == 0 {
		util.OK(c, map[string]interface{}{"configured": true, "project_name": cfg.ProjectName, "groups": visibleGroups, "items": []interface{}{}})
		return
	}
	result, err := callSeedanceArk(cfg, "ListAssets", map[string]interface{}{
		"Filter":     map[string]interface{}{"GroupIds": groupIDs, "GroupType": "LivenessFace", "Statuses": []string{"Active", "Processing", "Failed"}},
		"PageNumber": 1, "PageSize": 100, "SortBy": "CreateTime", "SortOrder": "Desc", "ProjectName": cfg.ProjectName,
	})
	if err != nil {
		util.BadRequest(c, "读取火山真人素材失败："+err.Error())
		return
	}
	items := []map[string]interface{}{}
	if rawItems, ok := seedanceValue(result, "Items").([]interface{}); ok {
		for _, raw := range rawItems {
			item, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			items = append(items, map[string]interface{}{
				"id": seedanceString(item, "Id"), "name": seedanceString(item, "Name"),
				"url": seedanceString(item, "URL"), "group_id": seedanceString(item, "GroupId"),
				"asset_type": seedanceString(item, "AssetType"), "status": seedanceString(item, "Status"),
				"created_at": seedanceString(item, "CreateTime"),
			})
			assetID, groupID := seedanceString(item, "Id"), seedanceString(item, "GroupId")
			if assetID != "" && h.assets.OwnsSeedancePortraitGroup(c.Request.Context(), c.GetInt64("user_id"), groupID, cfg.ProjectName) {
				_ = h.assets.SaveSeedancePortraitAsset(c.Request.Context(), c.GetInt64("user_id"), groupID, assetID, seedanceString(item, "AssetType"))
			}
		}
	}
	util.OK(c, map[string]interface{}{"configured": true, "project_name": cfg.ProjectName, "groups": visibleGroups, "items": items})
}

func (h *Handler) CreateSeedancePortraitAsset(c *gin.Context) {
	cfg, ok := h.seedancePortraitConfig(c)
	if !ok {
		return
	}
	var input struct {
		GroupID    string `json:"group_id"`
		LocalAsset string `json:"local_asset_id"`
		Name       string `json:"name"`
		AssetType  string `json:"asset_type"`
	}
	if c.ShouldBindJSON(&input) != nil || strings.TrimSpace(input.GroupID) == "" || strings.TrimSpace(input.LocalAsset) == "" {
		util.BadRequest(c, "素材组和本地素材不能为空")
		return
	}
	if !h.assets.OwnsSeedancePortraitGroup(c.Request.Context(), c.GetInt64("user_id"), input.GroupID, cfg.ProjectName) {
		util.Forbidden(c, "无权向该真人素材组上传素材")
		return
	}
	_, objectKey, asset, err := h.assets.Get(c.Request.Context(), c.GetInt64("user_id"), input.LocalAsset)
	if err != nil || asset == nil {
		util.BadRequest(c, "本地素材不存在")
		return
	}
	assetType := strings.Title(strings.ToLower(strings.TrimSpace(input.AssetType)))
	if assetType != "Image" && assetType != "Video" {
		util.BadRequest(c, "真人素材仅支持图片或视频")
		return
	}
	if strings.ToLower(asset.Kind) != strings.ToLower(assetType) {
		util.BadRequest(c, "素材文件类型与上传类型不匹配")
		return
	}
	sourceURL := h.storageURL(objectKey)
	parsedURL, parseErr := url.Parse(sourceURL)
	host := ""
	if parsedURL != nil {
		host = parsedURL.Hostname()
	}
	hostIP := net.ParseIP(host)
	if parseErr != nil || parsedURL == nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || host == "localhost" || (hostIP != nil && (hostIP.IsLoopback() || hostIP.IsPrivate())) {
		util.BadRequest(c, "素材必须具有公网可访问的 HTTP(S) 地址")
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" && asset.Name != nil {
		name = strings.TrimSpace(*asset.Name)
	}
	if len([]rune(name)) > 64 {
		name = string([]rune(name)[:64])
	}
	result, err := callSeedanceArk(cfg, "CreateAsset", map[string]interface{}{
		"GroupId": input.GroupID, "URL": sourceURL, "AssetType": assetType,
		"Name": name, "ProjectName": cfg.ProjectName,
	})
	if err != nil {
		util.BadRequest(c, "上传火山真人素材失败："+err.Error())
		return
	}
	assetID := seedanceString(result, "Id")
	if assetID == "" {
		util.BadRequest(c, "火山方舟未返回素材 ID")
		return
	}
	if err := h.assets.SaveSeedancePortraitAsset(c.Request.Context(), c.GetInt64("user_id"), input.GroupID, assetID, assetType); err != nil {
		util.InternalError(c, "保存真人素材归属失败")
		return
	}
	util.Created(c, map[string]interface{}{"id": assetID, "group_id": input.GroupID, "asset_type": assetType, "status": "Processing"})
}

func (h *Handler) ImportSeedancePortraitAsset(c *gin.Context) {
	cfg, ok := h.seedancePortraitConfig(c)
	if !ok {
		return
	}
	var input struct {
		AssetID string `json:"asset_id"`
	}
	if c.ShouldBindJSON(&input) != nil {
		util.BadRequest(c, "素材 ID 不能为空")
		return
	}
	assetID := strings.TrimPrefix(strings.TrimSpace(input.AssetID), "asset://")
	if assetID == "" {
		util.BadRequest(c, "素材 ID 不能为空")
		return
	}
	result, err := callSeedanceArk(cfg, "GetAsset", map[string]interface{}{"Id": assetID, "ProjectName": cfg.ProjectName})
	if err != nil {
		util.BadRequest(c, "读取火山真人素材失败："+err.Error())
		return
	}
	groupID := seedanceString(result, "GroupId")
	if !h.assets.OwnsSeedancePortraitGroup(c.Request.Context(), c.GetInt64("user_id"), groupID, cfg.ProjectName) {
		util.Forbidden(c, "该素材不属于当前账号的真人素材组")
		return
	}
	if status := seedanceString(result, "Status"); status != "Active" {
		util.BadRequest(c, "真人素材尚不可用，当前状态："+status)
		return
	}
	assetType := seedanceString(result, "AssetType")
	if err := h.assets.SaveSeedancePortraitAsset(c.Request.Context(), c.GetInt64("user_id"), groupID, assetID, assetType); err != nil {
		util.InternalError(c, "保存真人素材归属失败")
		return
	}
	util.OK(c, map[string]interface{}{"id": assetID, "group_id": groupID, "asset_type": assetType, "status": "Active"})
}
