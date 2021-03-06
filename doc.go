package eridanus

//go:generate protoc --go_out=paths=source_relative:. eridanus.proto

// import "golang.org/x/net/xsrftoken" // XSSP

// DefaultParsers provides default parsers.
func DefaultParsers() []*Parser {
	return []*Parser{
		{Name: "hf consent",
			Type: ParseResultType_FOLLOW,
			Operations: []*Parser_Operation{
				{Type: Parser_Operation_XPATH, Value: `//a[@id='frontPage_link']/@href`},
				{Type: Parser_Operation_SUFFIX, Value: "&size=728"},
			},
			Urls: []string{
				"http://www.hentai-foundry.com/pictures/user/Calm/801362/Patreon-70",
				"http://hentai-foundry.com/user/Calm/profile",
				"http://www.hentai-foundry.com/pictures/user/Calm",
			},
		},
		{Name: "hf next",
			Type: ParseResultType_FOLLOW,
			Operations: []*Parser_Operation{
				{Type: Parser_Operation_XPATH, Value: `//*[@id="yw2"]/li[contains(@class, 'next')]/a/@href`},
			},
			Urls: []string{
				"http://www.hentai-foundry.com/pictures/user/Calm",
			},
		},
		{Name: "hf post",
			Type: ParseResultType_FOLLOW,
			Operations: []*Parser_Operation{
				{Type: Parser_Operation_XPATH, Value: `//div[@id="yw0"]//a[contains(@class, 'thumbLink')]/@href`},
			},
			Urls: []string{
				"http://hentai-foundry.com/user/Calm/profile",
				"http://www.hentai-foundry.com/pictures/user/Calm",
			},
		},
		{Name: "hf content @src",
			Type: ParseResultType_CONTENT,
			Operations: []*Parser_Operation{
				{Type: Parser_Operation_XPATH, Value: `//*[@id="picBox"]//img/@src`},
			},
			Urls: []string{
				"http://www.hentai-foundry.com/pictures/user/Calm/801362/Patreon-70",
			},
		},
		{Name: "hf content @onclick",
			Type: ParseResultType_CONTENT,
			Operations: []*Parser_Operation{
				{Type: Parser_Operation_XPATH, Value: `//*[@id="picBox"]//img/@onclick`},
				{Type: Parser_Operation_REGEX, Value: `//pictures.hentai-foundry[^"']+`},
			},
			Urls: []string{
				"http://www.hentai-foundry.com/pictures/user/Calm/801362/Patreon-70",
			},
		},
		{Name: "hf content tags",
			Type: ParseResultType_TAG,
			Operations: []*Parser_Operation{
				{Type: Parser_Operation_XPATH, Value: `//a[@rel="tag"]`},
			},
			Urls: []string{
				"http://www.hentai-foundry.com/pictures/user/Calm/801362/Patreon-70",
			},
		},
		{Name: "hf content creator",
			Type: ParseResultType_TAG,
			Operations: []*Parser_Operation{
				{Type: Parser_Operation_XPATH, Value: `//*[@id="picBox"]//a`},
				{Type: Parser_Operation_PREFIX, Value: "creator:"},
			},
			Urls: []string{
				"http://www.hentai-foundry.com/pictures/user/Calm/801362/Patreon-70",
			},
		},
	}
}

// DefaultClasses provides default classes.
func DefaultClasses() []*URLClass {
	return []*URLClass{
		{Name: "Hentai-Foundry Post",
			Class:  URLClass_POST,
			Domain: "hentai-foundry.com",
			Path: []*StringMatcher{
				{Value: "pictures"},
				{Value: "user"},
				{Type: StringMatcher_REGEX, Value: "[A-Za-z0-9_-]+"},
				{Type: StringMatcher_REGEX, Value: `\d+`},
				{Type: StringMatcher_REGEX, Value: "[^/]+|"},
			},
			MatchSubdomain: true,
			AllowSubdomain: true,
		},
		{Name: "Hentai-Foundry Gallery",
			Class:  URLClass_LIST,
			Domain: "hentai-foundry.com",
			Path: []*StringMatcher{
				{Value: "pictures"},
				{Value: "user"},
				{Type: StringMatcher_REGEX, Value: "[A-Za-z0-9_-]+"},
			},
			MatchSubdomain: true,
			AllowSubdomain: true,
		},
		{Name: "Hentai-Foundry Profile",
			Class:  URLClass_LIST,
			Domain: "hentai-foundry.com",
			Path: []*StringMatcher{
				{Value: "user"},
				{Type: StringMatcher_REGEX, Value: "[A-Za-z0-9_-]+"},
				{Value: "profile"},
			},
			MatchSubdomain: true,
			AllowSubdomain: true,
		},
	}
}
