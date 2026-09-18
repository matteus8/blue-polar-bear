import zenoh
import time

vehicle_type = "drone"
team = "blue"
drone_name = "bravo"

session = zenoh.open(zenoh.Config())
topic = f"{vehicle_type}/{team}/{drone_name}/telemetry"

# Declare Publisher
pub = session.declare_publisher(topic)

# Track current state
last_status = f"{drone_name.capitalize()} initialized, awaiting mission"

# Declare Queryable: Answer on-demand queries
def on_query(query):
    print(f"[QUERY RECEIVED] Ground station asked for status on: {query.key_expr}")
    # reply with the last known status
    query.reply(query.key_expr, last_status)

queryable = session.declare_queryable(topic, on_query)
print(f"Drone {drone_name.capitalize()} ready. Publisher and Queryable active on: {topic}")

# Send updates every 2 seconds
x = 0
try:
    while True:
        x += 1
        last_status = f"{drone_name.capitalize()} airborne | Battery: 95% | Step: {x}"
        print(f"Publishing: {last_status}")
        pub.put(last_status)
        time.sleep(2)
except KeyboardInterrupt:
    print("\nLanding drone...")
finally:
    session.close()
