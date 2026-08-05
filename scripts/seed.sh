#!/usr/bin/env bash
# Seeds a few sample games so the dashboard isn't empty.
# Steam titles get real Steam cover art + steam_appid; the rest get a
# text placeholder (they're not on Steam).
# Usage: make seed   (or run this script directly)
set -euo pipefail

API="${API:-http://localhost:8080}"
placeholder() { # placeholder cover for non-Steam titles
  echo "https://placehold.co/300x450/18181b/ffffff?text=$(echo "$1" | sed 's/ /+/g')"
}

post() { # title status rating platform year genre [steamAppId]
  local title="$1" status="$2" rating="$3" platform="$4" year="$5" genre="$6"
  local appid="${7:-}"
  [ -z "$year" ] && year=null
  if [ -n "$appid" ]; then
    local cover="https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/$appid/library_600x900.jpg"
  else
    local cover
    cover="$(placeholder "$title")"
    appid=null
  fi
  curl -sf -X POST "$API/api/games" -H 'Content-Type: application/json' -d "{
    \"title\": \"$title\",
    \"status\": \"$status\",
    \"rating\": $rating,
    \"platform\": \"$platform\",
    \"year\": $year,
    \"genre\": \"$genre\",
    \"coverUrl\": \"$cover\",
    \"steamAppId\": $appid,
    \"notes\": \"Sample entry — edit or delete me anytime.\"
  }" >/dev/null && echo "  + $title"
}

echo "Seeding $API ..."
post "Elden Ring"            played   5 "PC"                 2022 "Action RPG"       1245620
post "The Witcher 3"         played   5 "PlayStation 5"      2015 "Action RPG"       292030
post "Stardew Valley"        played   4 "PC"                 2016 "Simulation"       413150
post "Hollow Knight"         played   5 "Nintendo Switch"    2017 "Metroidvania"     367520
post "Cyberpunk 2077"        played   3 "PC"                 2020 "Action RPG"       1091500
post "Zelda: Tears of the Kingdom" playing 0 "Nintendo Switch" 2023 "Adventure"
post "Balatro"               playing  0 "Mobile (iOS)"       2024 "Roguelike"        2379780
post "Silksong"              wishlist 0 "Nintendo Switch"    "" "Metroidvania"       1030300
post "GTA VI"                wishlist 0 "PlayStation 5"      "" "Open World"
post "Half-Life 3"           wishlist 0 "PC"                 "" "FPS"
post "Baldur's Gate 3"       purchased 0 "PC"                2023 "CRPG"              1086940
post "Starfield"             dropped   2 "PC"                2023 "Space RPG"
echo "Done."
