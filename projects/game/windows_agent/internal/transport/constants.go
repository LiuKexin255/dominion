package transport

import (
	gw "dominion/projects/game/gateway"
)

// AgentRole is the Windows Agent's role in the game protocol.
const AgentRole = gw.GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT

// MimeTypeMP4 is the MIME type for fMP4 video segments with H.264 codec.
const MimeTypeMP4 = "video/mp4; codecs=\"avc1.64001f\""
