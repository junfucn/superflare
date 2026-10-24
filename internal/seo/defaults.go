package seo

// Built-in copy used when the operator has not configured SEO text and the
// i18n bundle has no override. Keeping this in Go means the endpoints and the
// head block always emit useful text, even for deployments that never touch
// the new settings page.
var builtinCopy = map[string]map[string]string{
	"zh": {
		"seo_default_description":    "SuperFlare 是一款面向个人、兼具极致性能与极低资源开销的轻量化自托管导航首页，主打零散书签整理、内网服务快速访问与快捷导航管理。",
		"seo_desc_home":              "SuperFlare 个人自托管导航首页：集中整理常用书签与应用，快速访问内网服务，支持多主题、全键盘操作与极低资源占用。",
		"seo_desc_apps":              "SuperFlare 应用导航：按目录分组展示自托管的常用应用与服务入口，支持图标与简介，一站直达内网与外网服务。",
		"seo_desc_bookmarks":         "SuperFlare 书签导航：按分类整理的个人书签库，支持加密链接、本地地址与图标自动解析。",
		"seo_desc_help":              "SuperFlare 使用帮助：了解首页各功能模块、快捷键、搜索方式与个性化配置说明。",
		"seo_keywords_default":       "SuperFlare,导航首页,自托管,书签管理,内网导航,个人主页,开源导航,start page,bookmark manager,self-hosted",
		"seo_llms_summary":           "SuperFlare 是一款轻量级、自托管的个人导航首页，用于集中整理书签、应用与内网服务入口。",
		"seo_llms_section_pages":     "页面",
		"seo_llms_section_apps":      "应用",
		"seo_llms_section_bookmarks": "书签",
	},
	"en": {
		"seo_default_description":    "SuperFlare is a lightweight, self-hosted navigation homepage for personal use: organize scattered bookmarks, reach intranet services fast, and manage a personal start page with minimal resource usage.",
		"seo_desc_home":              "SuperFlare, your self-hosted start page: collect frequently used bookmarks and applications, jump into intranet services, and manage everything with multiple themes and near-zero resource usage.",
		"seo_desc_apps":              "SuperFlare application launcher: browse self-hosted apps grouped by directory, each with icons and descriptions, and reach internal or public services in one click.",
		"seo_desc_bookmarks":         "SuperFlare bookmark navigation: a categorised personal bookmark library with encrypted links, local addresses and automatic icon resolution.",
		"seo_desc_help":              "SuperFlare help: learn how each home page module works, plus keyboard shortcuts, search modes and personalisation options.",
		"seo_keywords_default":       "SuperFlare,start page,self-hosted navigation,bookmark manager,intranet dashboard,personal homepage,open source start page",
		"seo_llms_summary":           "SuperFlare is a lightweight, self-hosted personal navigation homepage that organises bookmarks, applications and intranet services.",
		"seo_llms_section_pages":     "Pages",
		"seo_llms_section_apps":      "Applications",
		"seo_llms_section_bookmarks": "Bookmarks",
	},
}

// builtinTranslation resolves copy for a locale with a zh default fallback.
func builtinTranslation(locale, key string) string {
	if bundle, ok := builtinCopy[locale]; ok {
		if value, ok := bundle[key]; ok {
			return value
		}
	}
	if bundle, ok := builtinCopy["zh"]; ok {
		return bundle[key]
	}
	return ""
}
