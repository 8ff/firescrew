// Update the SVG/CSS based on screen width
function updateResponsiveContent() {
    var background = document.querySelector('.background');
    var dynamicCss = document.getElementById('dynamic-css');
    var screenWidth = window.innerWidth;

    if (screenWidth <= 480) {
        background.data = "svg/mobile.svg";
        dynamicCss.href = "css/mobile.css";
        console.log("mobile");
    } else if (screenWidth <= 768) {
        background.data = "svg/tablet.svg";
        dynamicCss.href = "css/tablet.css";
        console.log("tablet");
    } else {
        background.data = "svg/desktop.svg";
        dynamicCss.href = "css/desktop.css";
        console.log("desktop");
    }
}

// Generate coordinates for area
function drawRectangle(containerClass) {
    var drawing = false;
    var startX, startY;
    var container = document.querySelector('.' + containerClass);
    var selection = document.createElement('div');
    selection.id = 'selection';
    selection.hidden = true;
    selection.style.position = 'absolute';
    container.appendChild(selection);

    function start(event) {
        if (event.touches) event = event.touches[0];
        drawing = true;
        startX = event.clientX - container.offsetLeft;
        startY = event.clientY - container.offsetTop;
        selection.style.left = startX + 'px';
        selection.style.top = startY + 'px';
        selection.style.width = '0';
        selection.style.height = '0';
        selection.hidden = false;
    }

    function move(event) {
        if (!drawing) return;
        if (event.touches) event = event.touches[0];
        var currentX = event.clientX - container.offsetLeft;
        var currentY = event.clientY - container.offsetTop;
        selection.style.width = Math.abs(currentX - startX) + 'px';
        selection.style.height = Math.abs(currentY - startY) + 'px';
        selection.style.left = Math.min(currentX, startX) + 'px';
        selection.style.top = Math.min(currentY, startY) + 'px';
    }

    function end(event) {
        drawing = false;
        generateCSS();
    }

    container.addEventListener('mousedown', start);
    container.addEventListener('mousemove', move);
    container.addEventListener('mouseup', end);

    container.addEventListener('touchstart', start);
    container.addEventListener('touchmove', move);
    container.addEventListener('touchend', end);

    function generateCSS() {
        var screenWidth = window.innerWidth;
        var verticalOffsetFactor;

        // Define verticalOffsetFactor based on screen width
        if (screenWidth <= 480) {
            verticalOffsetFactor = 1.2; // Value for mobile
        } else if (screenWidth <= 768) {
            verticalOffsetFactor = 1.38; // Value for tablet
        } else {
            verticalOffsetFactor = 1.8; // Value for desktop
        }

        var marginTop = (parseInt(selection.style.top) / container.offsetHeight * 100) * verticalOffsetFactor;
        var css = `.sample {
            position: absolute;
            background-color: red;
            top: 0;
            left: 0;
            margin-top: ${marginTop.toFixed(2)}%;
            margin-left: ${(parseInt(selection.style.left) / container.offsetWidth * 100).toFixed(2)}%;
            width: ${(parseInt(selection.style.width) / container.offsetWidth * 100).toFixed(2)}%;
            height: ${(parseInt(selection.style.height) / container.offsetHeight * 100).toFixed(2)}%;
        }`;

        console.log(css);
    }

}

document.querySelectorAll('.copy-code').forEach(function(codeBlock) {
    codeBlock.addEventListener('click', function() {
      var code = codeBlock.querySelector('code').innerText;
      var textArea = document.createElement('textarea');
      textArea.value = code;
      document.body.appendChild(textArea);
      textArea.select();
      document.execCommand('copy');
      document.body.removeChild(textArea);
  
      // Show the "Code copied!" message
      var copyMessage = codeBlock.querySelector('.copy-message');
      copyMessage.hidden = false;
      setTimeout(function() {
        copyMessage.hidden = true;
      }, 2000); // Hide the message after 2 seconds
    });

    codeBlock.addEventListener('hover', function() {
        var copyMessage = codeBlock.querySelector('.copy-message');
        copyMessage.hidden = false;
        console.log("hover")
    });
  });
  

// Update the SVG and CSS on page load
// updateResponsiveContent();

// Update the SVG and CSS on window resize
// window.addEventListener('resize', updateResponsiveContent);


// Enable area selection
// drawRectangle('contentWrapper');