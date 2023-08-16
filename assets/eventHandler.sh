#!/bin/bash

# Check if jq is installed
if ! command -v jq &> /dev/null
then
    echo "jq could not be found, please install it to use this script"
    exit 1
fi

# Read JSON data from stdin
json=$(cat)

# Extract values using jq
eventType=$(echo "$json" | jq -r '.type')
timestamp=$(echo "$json" | jq -r '.timestamp')
id=$(echo "$json" | jq -r '.id')
camera_name=$(echo "$json" | jq -r '.camera_name')

# Use a case statement to handle different event types
case $eventType in
  "motion_start")
    echo "Motion started event detected"
    echo "Timestamp: $timestamp"
    echo "ID: $id"
    echo "Camera Name: $camera_name"
    # Add code here to handle motion_started events
    ;;
  "motion_end")
    echo "Motion stopped event detected"
    # Add code here to handle motion_stopped events
    ;;
  "motion_update")
    echo "Motion update event detected"
    # Add code here to handle motion_update events
    ;;
  "objects_detected")
    echo "Objects detected event detected"
    # Add code here to handle objects_detected events
    ;;
  *)
    echo "Unknown event type: $eventType"
    # Add code here to handle unknown event types
    ;;
esac
