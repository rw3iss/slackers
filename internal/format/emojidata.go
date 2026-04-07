package format

// EmojiEntry is a single emoji with its shortcode and unicode.
type EmojiEntry struct {
	Code  string // shortcode (e.g. "smile")
	Emoji string // unicode character (e.g. "😄")
}

// EmojiCategory groups emojis under a named tab.
type EmojiCategory struct {
	Name  string       // display name
	Icon  string       // tab icon (emoji)
	Items []EmojiEntry // ordered list
}

// Categories returns all emoji categories with their entries.
// Pre-computed at package init time for fast access.
var Categories []EmojiCategory

func init() {
	Categories = []EmojiCategory{
		{Name: "Smileys", Icon: "😀", Items: []EmojiEntry{
			{"smile", "😄"}, {"smiley", "😃"}, {"grinning", "😀"}, {"grin", "😁"},
			{"laughing", "😆"}, {"sweat_smile", "😅"}, {"rofl", "🤣"}, {"joy", "😂"},
			{"slightly_smiling_face", "🙂"}, {"upside_down_face", "🙃"}, {"wink", "😉"}, {"blush", "😊"},
			{"innocent", "😇"}, {"heart_eyes", "😍"}, {"kissing_heart", "😘"}, {"kissing", "😗"},
			{"relaxed", "☺️"}, {"yum", "😋"}, {"stuck_out_tongue", "😛"}, {"stuck_out_tongue_winking_eye", "😜"},
			{"sunglasses", "😎"}, {"hugging_face", "🤗"}, {"thinking_face", "🤔"}, {"neutral_face", "😐"},
			{"expressionless", "😑"}, {"no_mouth", "😶"}, {"rolling_eyes", "🙄"}, {"smirk", "😏"},
			{"persevere", "😣"}, {"disappointed", "😞"}, {"confused", "😕"}, {"worried", "😟"},
			{"angry", "😠"}, {"rage", "😡"}, {"cry", "😢"}, {"sob", "😭"},
			{"scream", "😱"}, {"fearful", "😨"}, {"cold_sweat", "😰"}, {"sweat", "😓"},
			{"sleeping", "😴"}, {"sleepy", "😪"}, {"dizzy_face", "😵"}, {"zipper_mouth_face", "🤐"},
			{"mask", "😷"}, {"nerd_face", "🤓"}, {"skull", "💀"}, {"ghost", "👻"},
			{"alien", "👽"}, {"robot_face", "🤖"}, {"poop", "💩"}, {"clown_face", "🤡"},
			{"see_no_evil", "🙈"}, {"hear_no_evil", "🙉"}, {"speak_no_evil", "🙊"},
		}},
		{Name: "Hands", Icon: "👋", Items: []EmojiEntry{
			{"wave", "👋"}, {"raised_hand", "✋"}, {"vulcan_salute", "🖖"}, {"ok_hand", "👌"},
			{"v", "✌️"}, {"crossed_fingers", "🤞"}, {"metal", "🤘"}, {"call_me_hand", "🤙"},
			{"point_left", "👈"}, {"point_right", "👉"}, {"point_up", "👆"}, {"point_down", "👇"},
			{"thumbsup", "👍"}, {"thumbsdown", "👎"}, {"fist", "✊"}, {"facepunch", "👊"},
			{"fist_left", "🤛"}, {"fist_right", "🤜"}, {"clap", "👏"}, {"raised_hands", "🙌"},
			{"open_hands", "👐"}, {"handshake", "🤝"}, {"pray", "🙏"}, {"muscle", "💪"},
			{"eyes", "👀"}, {"brain", "🧠"}, {"writing_hand", "✍️"}, {"nail_care", "💅"},
			{"selfie", "🤳"}, {"dancer", "💃"}, {"man_dancing", "🕺"}, {"running", "🏃"},
		}},
		{Name: "Hearts", Icon: "❤️", Items: []EmojiEntry{
			{"heart", "❤️"}, {"orange_heart", "🧡"}, {"yellow_heart", "💛"}, {"green_heart", "💚"},
			{"blue_heart", "💙"}, {"purple_heart", "💜"}, {"black_heart", "🖤"}, {"white_heart", "🤍"},
			{"broken_heart", "💔"}, {"heartbeat", "💓"}, {"heartpulse", "💗"}, {"sparkling_heart", "💖"},
			{"cupid", "💘"}, {"gift_heart", "💝"}, {"fire", "🔥"}, {"star", "⭐"},
			{"star2", "🌟"}, {"sparkles", "✨"}, {"boom", "💥"}, {"zap", "⚡"},
			{"rainbow", "🌈"}, {"100", "💯"}, {"speech_balloon", "💬"}, {"thought_balloon", "💭"},
		}},
		{Name: "Objects", Icon: "💡", Items: []EmojiEntry{
			{"tada", "🎉"}, {"confetti_ball", "🎊"}, {"balloon", "🎈"}, {"gift", "🎁"},
			{"trophy", "🏆"}, {"medal", "🏅"}, {"crown", "👑"}, {"gem", "💎"},
			{"moneybag", "💰"}, {"dollar", "💵"}, {"bulb", "💡"}, {"wrench", "🔧"},
			{"hammer", "🔨"}, {"gear", "⚙️"}, {"link", "🔗"}, {"pushpin", "📌"},
			{"paperclip", "📎"}, {"scissors", "✂️"}, {"lock", "🔒"}, {"unlock", "🔓"},
			{"key", "🔑"}, {"bell", "🔔"}, {"mega", "📣"}, {"email", "📧"},
			{"memo", "📝"}, {"book", "📖"}, {"books", "📚"}, {"folder", "📁"},
			{"calendar", "📅"}, {"phone", "📱"}, {"computer", "💻"}, {"mag", "🔍"},
		}},
		{Name: "Status", Icon: "✅", Items: []EmojiEntry{
			{"white_check_mark", "✅"}, {"heavy_check_mark", "✔️"}, {"x", "❌"},
			{"heavy_plus_sign", "➕"}, {"heavy_minus_sign", "➖"}, {"exclamation", "❗"},
			{"question", "❓"}, {"warning", "⚠️"}, {"no_entry", "⛔"}, {"no_entry_sign", "🚫"},
			{"stop_sign", "🛑"}, {"construction", "🚧"}, {"red_circle", "🔴"}, {"orange_circle", "🟠"},
			{"yellow_circle", "🟡"}, {"green_circle", "🟢"}, {"blue_circle", "🔵"}, {"white_circle", "⚪"},
			{"black_circle", "⚫"}, {"large_blue_diamond", "🔷"}, {"large_orange_diamond", "🔶"},
		}},
		{Name: "Arrows", Icon: "➡️", Items: []EmojiEntry{
			{"arrow_up", "⬆️"}, {"arrow_down", "⬇️"}, {"arrow_left", "⬅️"}, {"arrow_right", "➡️"},
			{"arrow_upper_right", "↗️"}, {"arrow_lower_right", "↘️"}, {"arrow_upper_left", "↖️"},
			{"arrow_lower_left", "↙️"}, {"arrows_counterclockwise", "🔄"},
			{"leftwards_arrow_with_hook", "↩️"}, {"arrow_right_hook", "↪️"},
		}},
		{Name: "Food", Icon: "🍕", Items: []EmojiEntry{
			{"coffee", "☕"}, {"tea", "🍵"}, {"beer", "🍺"}, {"beers", "🍻"},
			{"wine_glass", "🍷"}, {"cocktail", "🍸"}, {"pizza", "🍕"}, {"hamburger", "🍔"},
			{"taco", "🌮"}, {"burrito", "🌯"}, {"popcorn", "🍿"}, {"cake", "🎂"},
			{"cookie", "🍪"}, {"doughnut", "🍩"}, {"ice_cream", "🍨"}, {"apple", "🍎"},
			{"banana", "🍌"}, {"watermelon", "🍉"}, {"avocado", "🥑"}, {"hot_pepper", "🌶️"},
		}},
		{Name: "Animals", Icon: "🐶", Items: []EmojiEntry{
			{"dog", "🐶"}, {"cat", "🐱"}, {"mouse_face", "🐭"}, {"hamster", "🐹"},
			{"rabbit", "🐰"}, {"fox_face", "🦊"}, {"bear", "🐻"}, {"panda_face", "🐼"},
			{"penguin", "🐧"}, {"chicken", "🐔"}, {"bird", "🐦"}, {"frog", "🐸"},
			{"monkey_face", "🐵"}, {"unicorn_face", "🦄"}, {"snake", "🐍"}, {"turtle", "🐢"},
			{"fish", "🐟"}, {"whale", "🐳"}, {"dolphin", "🐬"}, {"octopus", "🐙"},
			{"butterfly", "🦋"}, {"bug", "🐛"}, {"bee", "🐝"}, {"parrot", "🦜"},
		}},
		{Name: "Nature", Icon: "🌿", Items: []EmojiEntry{
			{"sunny", "☀️"}, {"sun_with_face", "🌞"}, {"cloud", "☁️"}, {"rain_cloud", "🌧️"},
			{"snowflake", "❄️"}, {"tornado", "🌪️"}, {"ocean", "🌊"}, {"earth_americas", "🌎"},
			{"earth_africa", "🌍"}, {"earth_asia", "🌏"}, {"globe_with_meridians", "🌐"},
			{"cherry_blossom", "🌸"}, {"rose", "🌹"}, {"sunflower", "🌻"}, {"herb", "🌿"},
			{"seedling", "🌱"}, {"evergreen_tree", "🌲"}, {"cactus", "🌵"}, {"mushroom", "🍄"},
			{"fallen_leaf", "🍂"}, {"maple_leaf", "🍁"}, {"four_leaf_clover", "🍀"},
		}},
		{Name: "Travel", Icon: "🚀", Items: []EmojiEntry{
			{"rocket", "🚀"}, {"airplane", "✈️"}, {"car", "🚗"}, {"taxi", "🚕"},
			{"bus", "🚌"}, {"bike", "🚲"}, {"ship", "🚢"}, {"house", "🏠"},
			{"office", "🏢"}, {"hospital", "🏥"}, {"school", "🏫"}, {"tent", "⛺"},
		}},
		{Name: "Flags", Icon: "🏁", Items: []EmojiEntry{
			{"flag-us", "🇺🇸"}, {"flag-gb", "🇬🇧"}, {"flag-jp", "🇯🇵"}, {"flag-kr", "🇰🇷"},
			{"flag-de", "🇩🇪"}, {"flag-fr", "🇫🇷"}, {"flag-es", "🇪🇸"}, {"flag-it", "🇮🇹"},
			{"flag-br", "🇧🇷"}, {"flag-ca", "🇨🇦"}, {"flag-au", "🇦🇺"}, {"flag-in", "🇮🇳"},
		}},
	}
}

// AllEmojis returns a flat list of all emoji entries across all categories.
func AllEmojis() []EmojiEntry {
	var all []EmojiEntry
	for _, cat := range Categories {
		all = append(all, cat.Items...)
	}
	return all
}

// FindByCode returns the emoji entry for a shortcode, or nil.
func FindByCode(code string) *EmojiEntry {
	for _, cat := range Categories {
		for i, e := range cat.Items {
			if e.Code == code {
				return &cat.Items[i]
			}
		}
	}
	return nil
}
