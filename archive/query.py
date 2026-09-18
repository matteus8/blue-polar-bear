import zenoh

session = zenoh.open(zenoh.Config())
target_team = "blue"
selector = f"drone/{target_team}/*/telemetry"

print(f"Sending on-demand query for: {selector} ...")

# Ask all matching nodes across the network for their current status
replies = session.get(selector)

count = 0
for reply in replies:
    if reply.ok is not None:
        count += 1
        key = reply.ok.key_expr
        data = reply.ok.payload.to_bytes().decode()
        print(f"  [REPLY #{count}] From: {key} -> '{data}'")
    else:
        print(f"  [QUERY ERROR]: {reply.err}")

if count == 0:
    print("No drones responded. Are any drones running?")

session.close()