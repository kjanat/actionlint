local path = pandoc.path.join({ pandoc.path.directory(PANDOC_SCRIPT_FILE), "..", "VERSION" })
local file = assert(io.open(path))
local version = file:read("a"):match("^%s*(.-)%s*$")
file:close()

local function release_link(url)
	return (url:gsub("/blob/HEAD/docs/", "/blob/v" .. version .. "/docs/", 1))
end

function Meta(meta)
	meta.footer = "actionlint " .. version
	return meta
end

function Str(elem)
	elem.text = release_link(elem.text)
	return elem
end

function Link(elem)
	elem.target = release_link(elem.target)
	return elem
end
