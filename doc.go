package eridanus

//go:generate protoc --go_out=paths=source_relative:. eridanus.proto
//BREAK go:generate rsrc -manifest main.manifest -o rsrc.syso
//BREAK go:generate enumer -json -text -yaml -sql -type=ParserOutputType
//BREAK go:generate enumer -json -text -yaml -sql -type=ParserOpType
//BREAK go:generate enumer -json -text -yaml -sql -type=StringMatcherType
//BREAK go:generate enumer -json -text -yaml -sql -type=URLClassifierType

var defaultConfig = &PBConfig{
	LocalData: "",
	Parsers: []*Parser{
		{Name: "hf consent",
			Type: Parser_FOLLOW,
			Operations: []*Parser_Operation{
				{Value: "//a[@id='frontPage_link']/@href"},
				{Type: Parser_Operation_SUFFIX, Value: "&size=728"},
			},
			Urls: []string{
				"http://www.hentai-foundry.com/pictures/user/Calm/801362/Patreon-70",
				"http://hentai-foundry.com/user/Calm/profile",
				"http://www.hentai-foundry.com/pictures/user/Calm",
			},
		},
		{Name: "hf next",
			Type: Parser_FOLLOW,
			Operations: []*Parser_Operation{
				{Value: `//*[@id="yw2"]/li[contains(@class, 'next')]/a/@href`},
			},
			Urls: []string{
				"http://www.hentai-foundry.com/pictures/user/Calm",
			},
		},
		{Name: "hf post",
			Type: Parser_FOLLOW,
			Operations: []*Parser_Operation{
				{Value: `//div[@id="yw0"]//a[contains(@class, 'thumbLink')]/@href`},
			},
			Urls: []string{
				"http://hentai-foundry.com/user/Calm/profile",
				"http://www.hentai-foundry.com/pictures/user/Calm",
			},
		},
		{Name: "hf content @src",
			Type: Parser_CONTENT,
			Operations: []*Parser_Operation{
				{Value: `//*[@id="picBox"]//img/@src`},
			},
			Urls: []string{
				"http://www.hentai-foundry.com/pictures/user/Calm/801362/Patreon-70",
			},
		},
		{Name: "hf content @onclick",
			Type: Parser_CONTENT,
			Operations: []*Parser_Operation{
				{Value: `//*[@id="picBox"]//img/@onclick`},
				{Type: Parser_Operation_REGEX, Value: `//pictures.hentai-foundry[^']+`},
			},
			Urls: []string{
				"http://www.hentai-foundry.com/pictures/user/Calm/801362/Patreon-70",
			},
		},
		{Name: "hf content tags",
			Type: Parser_TAG,
			Operations: []*Parser_Operation{
				{Value: `//a[@rel="tag"]`},
			},
			Urls: []string{
				"http://www.hentai-foundry.com/pictures/user/Calm/801362/Patreon-70",
			},
		},
		{Name: "hf content creator",
			Type: Parser_TAG,
			Operations: []*Parser_Operation{
				{Value: `//*[@id="picBox"]//a`},
				{Type: Parser_Operation_PREFIX, Value: "creator:"},
			},
			Urls: []string{
				"http://www.hentai-foundry.com/pictures/user/Calm/801362/Patreon-70",
			},
		},
	},
	Classes: []*URLClassifier{
		{Name: "Hentai-Foundry Post",
			Class:  URLClassifier_POST,
			Domain: "hentai-foundry.com",
			Path: []*StringMatcher{
				{Value: "pictures"},
				{Value: "user"},
				{Type: MatcherType_REGEX, Value: "[A-Za-z0-9_-]+"},
				{Type: MatcherType_REGEX, Value: `\d+`},
				{Type: MatcherType_REGEX, Value: "[^/]+|"},
			},
			MatchSubdomain: true,
			AllowSubdomain: true,
		},
		{Name: "Hentai-Foundry Gallery",
			Class:  URLClassifier_LIST,
			Domain: "hentai-foundry.com",
			Path: []*StringMatcher{
				{Value: "pictures"},
				{Value: "user"},
				{Type: MatcherType_REGEX, Value: "[A-Za-z0-9_-]+"},
			},
			MatchSubdomain: true,
			AllowSubdomain: true,
		},
		{Name: "Hentai-Foundry Profile",
			Class:  URLClassifier_LIST,
			Domain: "hentai-foundry.com",
			Path: []*StringMatcher{
				{Value: "user"},
				{Type: MatcherType_REGEX, Value: "[A-Za-z0-9_-]+"},
				{Value: "profile"},
			},
			MatchSubdomain: true,
			AllowSubdomain: true,
		},
	},
}
