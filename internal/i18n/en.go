package i18n

// enCatalogs translate the interface's messages into English, by their
// Chinese. Each feature area keeps its own file so parallel changes rarely
// edit the same lines; a message used by several areas lives in enCommon.
// catalog_test.go keeps the catalogs complete, free of duplicates and free of
// messages no code shows any more.
var enCatalogs = []map[string]string{
	enCommon,
	enApp,
	enEditor,
	enDatabase,
	enWhiteboard,
	enWhiteboardAI,
	enOneBot,
	enSync,
}

// en is the merged lookup table T reads.
var en = func() map[string]string {
	n := 0
	for _, c := range enCatalogs {
		n += len(c)
	}
	out := make(map[string]string, n)
	for _, c := range enCatalogs {
		for zh, s := range c {
			out[zh] = s
		}
	}
	return out
}()
