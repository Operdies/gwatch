#!/bin/bash

# Recursively watch all file events in the current working directory
# and pipe them into this while-loop line by line.
# Since the default command is 'echo %e %f', we can easily read them into an array
gwatch | while read -a msg; do 
  event="${msg[0]}"
  file="${msg[1]}"

  # From here, it's easy to match on the event, and respond to the change
  # Be wary that under some circumstances, events can be joined. E.g. the event could be "CHMOD|WRITE"
  case "$event" in
    *WRITE*)
      echo "Someone wrote $file"
      ;;
    *REMOVE*)
      echo "Someone deleted $file"
      ;;
    *)
      echo "$event $file"
      ;;
  esac
done
