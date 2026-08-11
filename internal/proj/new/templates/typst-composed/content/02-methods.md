# Methods

## Analysis

Math is LaTeX, exactly as you already write it: inline $r = .42$, display:

$$
\hat{y}_i = \beta_0 + \beta_1 x_i + \varepsilon_i, \qquad \varepsilon_i \sim \mathcal{N}(0, \sigma^2)
$$

Code fences render as code, never as prose — `@decorators` and the like are left alone inside them:

```python
@cache
def model(x, beta):
    return beta[0] + beta[1] * x
```
