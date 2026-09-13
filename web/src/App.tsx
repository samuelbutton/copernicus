export function App() {
  return (
    <>
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <header className="page-header">
        <span className="wordmark">Copernicus</span>
        <span className="eyebrow">Local simulation review</span>
      </header>
      <main id="main" tabIndex={-1}>
        <section className="intro" aria-labelledby="page-title">
          <p className="eyebrow">A small experiment. A clearer result.</p>
          <h1 id="page-title">Understand a driving test.</h1>
          <p className="lead">
            Explore how a vehicle’s braking decision changes what happens next.
            Copernicus is a learning project for choosing tests and comparing
            their results.
          </p>
          <a className="primary-link" href="#example">
            Explore the example <span aria-hidden="true">↓</span>
          </a>
        </section>
        <section
          id="example"
          className="example"
          aria-labelledby="example-title"
          tabIndex={-1}
        >
          <p className="eyebrow">The idea behind the project</p>
          <h2 id="example-title">Same obstacle. Different braking.</h2>
          <p>
            A vehicle approaches a stopped obstacle. Two braking rules lead to
            different outcomes.
          </p>
          <dl className="comparison">
            <div>
              <dt>Earlier braking</dt>
              <dd>The vehicle stops before contact.</dd>
            </div>
            <div>
              <dt>Later braking</dt>
              <dd>The vehicle reaches the obstacle.</dd>
            </div>
          </dl>
          <p className="muted">
            This is an explanation of the example, not a loaded test result.
          </p>
        </section>
        <aside className="availability" aria-labelledby="availability-title">
          <h2 id="availability-title">What you can do today</h2>
          <p>
            Read this introduction and explore the command help. Test selection
            and saved comparisons are not available yet.
          </p>
        </aside>
      </main>
      <footer className="page-footer">
        A learning example that runs on your own computer.
      </footer>
    </>
  );
}
