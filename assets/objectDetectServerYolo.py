#!/usr/bin/env python3
from ultralytics import YOLO
from PIL import Image
import io
import numpy as np
import socket
import json
import threading

# Load the YOLO model outside the main server loop so that it's done only once
model = YOLO("./yolov8n.pt")

def recvall(sock, count):
    buf = b''
    while count:
        newbuf = sock.recv(count)
        if not newbuf: return None
        buf += newbuf
        count -= len(newbuf)
    return buf

def handle_client(conn):
    # Read the frame length (assumed to be sent as a 4-byte integer)
    frame_len_bytes = conn.recv(4)
    frame_len = int.from_bytes(frame_len_bytes, 'big')

    # Read the frame data
    frame_data = recvall(conn, frame_len)

    if frame_data is None:
        print('Client closed connection.')
        conn.close()
        return

# Convert the raw bytes into an image
    image = Image.open(io.BytesIO(frame_data))

    # Convert the image into a NumPy array
    image_np = np.array(image)

    # Here you would process the frame data with the YOLO model
    results_list = model(image_np)  
    results = results_list[0]

    # Get the detected objects, their bounding boxes, and confidence scores
    predictions = []
    for idx, (box, conf, cls) in enumerate(zip(results.boxes.xyxy, results.boxes.conf, results.boxes.cls)):
        # Look up the class name in the names dictionary
        class_name = results.names[int(cls)]

        # Append the result to the predictions list
        predictions.append({
            'object': idx + 1,
            'class_name': class_name,
            'box': box.tolist(),
            'confidence': float(conf)
        })

    # Convert the predictions to a JSON string
    predictions_json = json.dumps(predictions)

    # Send the results back to the client
    # conn.sendall(predictions_json.encode())
    conn.sendall((predictions_json + '\n').encode())

    # Close the connection
    conn.close()

def main():
    LISTEN_ADDR = "0.0.0.0"
    LISTEN_PORT = 8555

    # Create a socket object
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)

    # Set the SO_REUSEADDR flag
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)

    # Bind the socket to a public host, and a port
    s.bind((LISTEN_ADDR, LISTEN_PORT))
    s.listen(5)

    print("Server is listening on %s:%d" % (LISTEN_ADDR, LISTEN_PORT))

    while True:
        # Establish a connection with the client
        conn, addr = s.accept()
        print(f"Got connection from {addr}")

        # Handle the client connection in a new thread
        thread = threading.Thread(target=handle_client, args=(conn,))
        thread.start()

if __name__ == "__main__":
    main()
