-- groff renders the constant-width font as plain text on a terminal, so pandoc's
-- default `\f[CR]` for inline code is invisible in a rendered man page.
function Code(elem)
	return pandoc.Strong(pandoc.Str(elem.text))
end
