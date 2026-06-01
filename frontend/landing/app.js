// Install Command Copy
function copyInstall() {
  const cmd = document.getElementById('install-cmd').textContent;
  const btn = document.getElementById('copy-btn');
  navigator.clipboard.writeText(cmd).then(() => {
    btn.textContent = 'copied';
    btn.classList.add('copied');
    setTimeout(() => { 
      btn.textContent = 'copy'; 
      btn.classList.remove('copied'); 
    }, 1800);
  }).catch(() => {
    btn.textContent = 'failed';
  });
}

// Dial & View Toggle Logic
const btnLanding = document.getElementById('btn-landing');
const btnJoin = document.getElementById('btn-join');
const indicator = document.querySelector('.dial-indicator');
const viewLanding = document.getElementById('view-landing');
const viewJoin = document.getElementById('view-join');
const sessionInput = document.getElementById('session-code');

function updateIndicator(activeBtn) {
  if (!indicator || !activeBtn) return;
  indicator.style.width = activeBtn.offsetWidth + 'px';
  indicator.style.transform = `translateX(${activeBtn.offsetLeft}px)`;
}

function switchView(view) {
  if (view === 'landing') {
    btnLanding.classList.add('active');
    btnJoin.classList.remove('active');
    
    viewJoin.classList.remove('active-view');
    setTimeout(() => {
      viewJoin.style.display = 'none';
      viewLanding.style.display = 'block';
      setTimeout(() => viewLanding.classList.add('active-view'), 10);
    }, 300);

    updateIndicator(btnLanding);
  } else {
    btnJoin.classList.add('active');
    btnLanding.classList.remove('active');
    
    viewLanding.classList.remove('active-view');
    setTimeout(() => {
      viewLanding.style.display = 'none';
      viewJoin.style.display = 'block';
      setTimeout(() => {
        viewJoin.classList.add('active-view');
        sessionInput.focus();
      }, 10);
    }, 300);

    updateIndicator(btnJoin);
  }
}

// Initialize indicator on load and handle resize
window.addEventListener('load', () => {
  setTimeout(() => updateIndicator(btnLanding), 50);
});

window.addEventListener('resize', () => {
  updateIndicator(btnLanding.classList.contains('active') ? btnLanding : btnJoin);
});

btnLanding.addEventListener('click', () => switchView('landing'));
btnJoin.addEventListener('click', () => switchView('join'));

// Form Submission
document.getElementById('join-form').addEventListener('submit', (e) => {
  e.preventDefault();
  const code = sessionInput.value.trim().toLowerCase();
  if (code.length === 5) {
    window.location.href = '/s/' + code;
  } else {
    sessionInput.style.borderColor = '#ff5f56';
    setTimeout(() => sessionInput.style.borderColor = '', 1000);
  }
});

// Filter input to alphanumeric
sessionInput.addEventListener('input', (e) => {
  e.target.value = e.target.value.replace(/[^a-zA-Z0-9]/g, '').toLowerCase();
});