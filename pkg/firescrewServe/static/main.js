let baseVideoUrl = "/rec/";
let baseImageUrl = "/images/";
let imageGrid = document.getElementById('imageGrid');
let modal = document.getElementById('myModal');
let videoPlayer = document.getElementById('videoPlayer');
let span = document.getElementsByClassName("close")[0];
let eventColorMap = {};


// Focus on the prompt input
window.onload = function () {
    let input = document.getElementById('promptInput');
    input.focus();
    input.setSelectionRange(input.value.length, input.value.length);
}

// Get eventInfo object
let eventInfo = document.getElementById('eventInfo');

function getEventColor(eventId) {
    if (!eventColorMap[eventId]) {
        const hue = Math.floor(Math.random() * 360); // Generate a number between 0 and 360
        const saturation = 100; // Set saturation to 100 for vibrant colors
        const lightness = 50; // Set lightness to 50 for vibrant colors
        const alpha = 0.7; // Adjust transparency
        const color = `hsla(${hue}, ${saturation}%, ${lightness}%, ${alpha})`;
        eventColorMap[eventId] = color;
    }
    return eventColorMap[eventId];
}

function getObjectIcon(objectType) {
    switch (objectType) {
        case 'car': return 'fas fa-car';
        case 'truck': return 'fas fa-truck';
        case 'person': return 'fas fa-user';
        case 'bicycle': return 'fas fa-bicycle';
        case 'motorcycle': return 'fas fa-motorcycle';
        case 'bus': return 'fas fa-bus';
        case 'cat': return 'fas fa-cat';
        case 'dog': return 'fas fa-dog';
        case 'boat': return 'fas fa-ship';
        default: return 'fas fa-question';
    }
}

function createImageElement(snapshot, item) {
    let imgDiv = document.createElement('div');
    imgDiv.classList.add("image-wrapper");

    let img = document.createElement('img');
    img.src = baseImageUrl + snapshot;

    // Add a background color based on the event
    let color = getEventColor(item.ID);
    img.style.boxShadow = `0 0 5px 1px ${color}`;  // NEW: Set the box-shadow color here.

    imgDiv.appendChild(img);

    // If there are objects, add the icon
    if (item.Objects && item.Objects.length > 0) {
        item.Objects.forEach(object => {
            let icon = document.createElement('i');
            icon.className = getObjectIcon(object.Class);
            imgDiv.appendChild(icon);
        });
    }

    return imgDiv;
}


function playVideo(videoFile, poster) {
    videoPlayer.poster = poster;  // Set the poster attribute
    videoPlayer.src = baseVideoUrl + videoFile;
    modal.style.display = "block";
}

function addInfoLabel(name, value, optClass) {
    let label = document.createElement('label');
    label.textContent = name + ': ' + value;
    label.classList.add("infoLabel");
    if (optClass) {
        label.classList.add(optClass);
    }
    eventInfo.appendChild(label);
}

function addPlainLabel(value, optClass) {
    let label = document.createElement('label');
    label.textContent = value;
    label.classList.add("infoLabel");
    if (optClass) {
        label.classList.add(optClass);
    }
    eventInfo.appendChild(label);
}

function formatDate(dateString) {
    // Create a new Date object
    let date = new Date(dateString);

    // Get the components of the date
    let day = String(date.getDate()).padStart(2, '0');
    let month = String(date.getMonth() + 1).padStart(2, '0'); // Months are 0-based, so add 1
    let year = String(date.getFullYear()).slice(2);
    let hours = String(date.getHours()).padStart(2, '0');
    let minutes = String(date.getMinutes()).padStart(2, '0');
    let seconds = String(date.getSeconds()).padStart(2, '0');

    // Format the date
    let formattedDate = `${day}/${month}/${year} ${hours}:${minutes}:${seconds}`;

    return formattedDate;
}

// function queryData() {
//     let promptValue = document.getElementById('promptInput').value;
//     // If promptValue == "" return
//     if (promptValue == "") {
//         return;
//     }

//     // Clear the image grid
//     imageGrid.innerHTML = '';

//     fetch('/api?prompt=' + encodeURIComponent(promptValue))
//         .then(response => response.json())
//         .then(data => {
//             // Log the data for debugging
//             console.log('Received data:', data);

//             // Then proceed as before
//             if (data && data.data) {
//                 data.data.forEach(item => {
//                     item.Snapshots.forEach(snapshot => {
//                         let imgDiv = document.createElement('div');
//                         imgDiv.classList.add("image-wrapper");

//                         let img = document.createElement('img');
//                         img.src = baseImageUrl + snapshot;

//                         // Add a background color based on the event
//                         let color = getEventColor(item.ID);
//                         img.style.boxShadow = `0 0 5px 1px ${color}`;

//                         imgDiv.appendChild(img);

//                         // If there are objects, add the icon
//                         if (item.Objects && item.Objects.length > 0) {
//                             let uniqueObjects = [];

//                             item.Objects.forEach(object => {
//                                 if (!uniqueObjects.includes(object.Class)) {
//                                     uniqueObjects.push(object.Class);

//                                     let icon = document.createElement('i');
//                                     icon.className = getObjectIcon(object.Class);
//                                     icon.classList.add("objectIcon");
//                                     imgDiv.appendChild(icon);
//                                 }
//                             });
//                         }

//                         img.addEventListener('click', function () {
//                             playVideo(item.VideoFile, baseImageUrl + snapshot);
//                             if (item.Objects && item.Objects.length > 0) {
//                                 // Go over all item.Objects and add Class/Confidence to eventInfo div
//                                 eventInfo.innerHTML = '';
//                                 // Add infoLabel with event ID
//                                 addInfoLabel('ID', item.ID, "infoLabelEventID");
//                                 // Add MotionStart time
//                                 // Convert MotionStart from 2023-07-28T16:35:52.161927-04:00 to 2023-07-28 16:35:52
//                                 newDate = formatDate(item.MotionStart)
//                                 addInfoLabel('T', newDate, "infoLabelTime");
//                                 // Add infoLabel with camera name
//                                 addInfoLabel('Cam', item.CameraName, "infoLabelCameraName");

//                                 // Reset the uniqueObjects array for eventInfo div
//                                 let uniqueObjects = [];

//                                 item.Objects.forEach(object => {
//                                     // Trim confidence to 2 decimals
//                                     // object.Confidence = Math.round(object.Confidence * 100) / 100;
//                                     // Add to uniqueObjects array is not already there
//                                     if (!uniqueObjects.includes(object.Class)) {
//                                         uniqueObjects.push(object.Class);
//                                     }
//                                 });

//                                 // Add uniqueObjects to eventInfo div
//                                 uniqueObjects.forEach(object => {
//                                     addPlainLabel(object);
//                                 });

//                             }
//                         });

//                         imageGrid.appendChild(imgDiv);
//                     });
//                 });
//             } else {
//                 console.error('Invalid data:', data);
//             }
//         })
//         .catch(error => console.error('Error:', error));
// }

function queryData() {
    let promptValue = document.getElementById('promptInput').value;
    // If promptValue == "" return
    if (promptValue == "") {
        return;
    }

    // Clear the image grid
    imageGrid.innerHTML = '';

    fetch('/api?prompt=' + encodeURIComponent(promptValue))
        .then(response => response.json())
        .then(data => {
            // Log the data for debugging
            console.log('Received data:', data);

            // Then proceed as before
            if (data && data.data) {
                data.data.forEach(item => {
                    item.Snapshots.forEach(snapshot => {
                        let imgDiv = document.createElement('div');
                        imgDiv.classList.add("image-wrapper");

                        let img = document.createElement('img');
                        img.src = baseImageUrl + snapshot;

                        // Add a background color based on the event
                        let color = getEventColor(item.ID);
                        img.style.boxShadow = `0 0 5px 1px ${color}`;

                        imgDiv.appendChild(img);

                        // Create a div for the icons
                        let iconsDiv = document.createElement('div');
                        iconsDiv.classList.add('icons');

                        // If there are objects, add the icon
                        if (item.Objects && item.Objects.length > 0) {
                            let uniqueObjects = [];

                            item.Objects.forEach(object => {
                                if (!uniqueObjects.includes(object.Class)) {
                                    uniqueObjects.push(object.Class);
                                }
                            });

                            uniqueObjects.forEach(objectClass => {
                                let icon = document.createElement('i');
                                icon.className = getObjectIcon(objectClass);
                                icon.classList.add("objectIcon");
                                iconsDiv.appendChild(icon);  // Append the icon to the iconsDiv
                            });
                        }

                        // Append the iconsDiv to the imgDiv
                        imgDiv.appendChild(iconsDiv);

                        img.addEventListener('click', function () {
                            playVideo(item.VideoFile, baseImageUrl + snapshot);
                            if (item.Objects && item.Objects.length > 0) {
                                // Go over all item.Objects and add Class/Confidence to eventInfo div
                                eventInfo.innerHTML = '';
                                // Add infoLabel with event ID
                                addInfoLabel('ID', item.ID, "infoLabelEventID");
                                // Add MotionStart time
                                // Convert MotionStart from 2023-07-28T16:35:52.161927-04:00 to 2023-07-28 16:35:52
                                newDate = formatDate(item.MotionStart)
                                addInfoLabel('T', newDate, "infoLabelTime");
                                // Add infoLabel with camera name
                                addInfoLabel('Cam', item.CameraName, "infoLabelCameraName");

                                // Reset the uniqueObjects array for eventInfo div
                                let uniqueObjects = [];

                                item.Objects.forEach(object => {
                                    // Trim confidence to 2 decimals
                                    // object.Confidence = Math.round(object.Confidence * 100) / 100;
                                    // Add to uniqueObjects array is not already there
                                    if (!uniqueObjects.includes(object.Class)) {
                                        uniqueObjects.push(object.Class);
                                    }
                                });

                                // Add uniqueObjects to eventInfo div
                                uniqueObjects.forEach(object => {
                                    addPlainLabel(object);
                                });

                            }
                        });

                        imageGrid.appendChild(imgDiv);
                    });
                });
            } else {
                console.error('Invalid data:', data);
            }
        })
        .catch(error => console.error('Error:', error));
}



promptInput.addEventListener('keydown', function (event) {
    if (event.key === "Enter") {
        event.preventDefault();
        queryData();
    }
});

// Every 30 seconds query the data
setInterval(queryData, 15000);

// When the user clicks on <span> (x), close the modal
span.onclick = function () {
    modal.style.display = "none";
}


// When the user clicks anywhere outside of the modal, close it
window.onclick = function (event) {
    if (event.target == modal) {
        modal.style.display = "none";
    }
}
