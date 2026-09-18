import zenoh
import time
import re

# 1. Open a session
session = zenoh.open(zenoh.Config())

# 2. Defind the callback to handle incoming messages
def process_incoming_telemetry(received_msg):
    text = received_msg.payload.to_bytes().decode()
    print(f"Received [{received_msg.key_expr}]: {text}")
    drone_name = str(received_msg.key_expr).split("/")[2].capitalize()
    if "Battery" in text:
        battery_percentage = text.split("|")[1].split(":")[1].strip()
        print(f"Battery percentage for {drone_name} is currently {battery_percentage}!")

# 3. Declare the subscriber using the key expression
subscriber = session.declare_subscriber("drone/*/*/telemetry", process_incoming_telemetry)

print("Listening for drone telemetry... Press Enter to exit")

input()

# 4. Clean up
session.close()