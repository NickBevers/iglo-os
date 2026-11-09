# Records the GET answers of the manager on the dev mock into
# web/src/demo/fixtures.json, for the demo build. Run it with the manager on
# 127.0.0.1:8080 and the current login code:
#
#   python3 -I dev/demo/record.py "$(go run ./dev/mock code | tr -dc '0-9' | tail -c 6)"
#
# The usage charts are left out. The demo makes those up, because their points
# are most of the data.
import datetime
import http.cookiejar
import json
import sys
import urllib.parse
import urllib.request

BASE = "http://127.0.0.1:8080/api"
OUT = "web/src/demo/fixtures.json"

opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
login = urllib.request.Request(
    BASE + "/auth/login",
    data=json.dumps({"username": "admin", "password": "iglo-dev-password", "code": sys.argv[1]}).encode(),
    headers={"Content-Type": "application/json", "X-Iglo-Request": "1"},
)
opener.open(login).read()

answers = {}


def get(path):
    try:
        with opener.open(BASE + path) as response:
            answers[path] = json.loads(response.read())
            return answers[path]
    except urllib.error.HTTPError:
        return None


for path in ["/auth/me", "/overview", "/apps", "/logs/tasks", "/updates", "/inbox", "/music/jobs", "/gaming",
             "/gaming/pairing", "/gaming/games", "/storage/bindings", "/setup", "/workloads", "/jellyfin",
             "/self-update", "/backups", "/tailscale", "/health"]:
    get(path)
for vmid in [0] + [guest["vmid"] for guest in answers["/overview"]["guests"]]:
    for priority in range(8):
        get(f"/logs/journal?vmid={vmid}&priority={priority}&lines=500")
    if vmid:
        get(f"/logs/docker?vmid={vmid}&lines=200")
for task in answers["/logs/tasks"]:
    get(f"/logs/tasks/{task['node']}/log?" + urllib.parse.urlencode({"upid": task["upid"]}))
for app in answers["/apps"]["apps"]:
    get(f"/apps/{app['id']}/vpn")

recorded = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
with open(OUT, "w") as file:
    json.dump({"recordedAt": recorded, "answers": answers}, file, indent=1, sort_keys=True)
print(len(answers), "answers in", OUT)
