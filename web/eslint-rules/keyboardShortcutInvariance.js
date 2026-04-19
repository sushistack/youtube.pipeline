const ENTER_KEYS = new Set(['enter'])
const ESCAPE_KEYS = new Set(['escape', 'esc'])

const PRIMARY_ACTIONS = new Set(['approve', 'confirm', 'primary', 'submit'])
const SECONDARY_ACTIONS = new Set([
  'back',
  'cancel',
  'close',
  'dismiss',
  'reject',
  'secondary',
])

const JSX_KEY_ATTRIBUTES = new Set(['onKeyDown', 'onKeyUp', 'onKeyPress'])

function readStaticString(node) {
  if (!node) {
    return null
  }

  if (node.type === 'Literal' && typeof node.value === 'string') {
    return node.value
  }

  if (node.type === 'TemplateLiteral' && node.expressions.length === 0) {
    return node.quasis[0]?.value.cooked ?? null
  }

  return null
}

function getObjectProperty(object_expression, names) {
  return object_expression.properties.find((property) => {
    if (property.type !== 'Property' || property.computed) {
      return false
    }

    if (property.key.type === 'Identifier') {
      return names.includes(property.key.name)
    }

    if (property.key.type === 'Literal' && typeof property.key.value === 'string') {
      return names.includes(property.key.value)
    }

    return false
  })
}

function normalizeSemanticValue(value) {
  return value?.trim().toLowerCase() ?? null
}

function classifyKey(value) {
  const normalized = normalizeSemanticValue(value)
  if (!normalized) {
    return null
  }
  if (ENTER_KEYS.has(normalized)) {
    return 'enter'
  }
  if (ESCAPE_KEYS.has(normalized)) {
    return 'escape'
  }
  return null
}

function isKeyMemberAccess(node) {
  if (!node || node.type !== 'MemberExpression' || node.computed) {
    return false
  }
  if (node.property.type !== 'Identifier') {
    return false
  }
  return node.property.name === 'key'
}

function extractKeyComparison(test) {
  if (!test || test.type !== 'BinaryExpression') {
    return null
  }
  if (test.operator !== '===' && test.operator !== '==') {
    return null
  }

  if (isKeyMemberAccess(test.left)) {
    return classifyKey(readStaticString(test.right))
  }
  if (isKeyMemberAccess(test.right)) {
    return classifyKey(readStaticString(test.left))
  }
  return null
}

function classifyCalleeAction(node) {
  if (!node) {
    return null
  }

  if (node.type === 'CallExpression') {
    return classifyCalleeAction(node.callee)
  }

  if (node.type === 'Identifier') {
    const name = node.name.toLowerCase()
    if (PRIMARY_ACTIONS.has(name)) {
      return 'primary'
    }
    if (SECONDARY_ACTIONS.has(name)) {
      return 'secondary'
    }
    for (const primary of PRIMARY_ACTIONS) {
      if (name.includes(primary)) {
        return 'primary'
      }
    }
    for (const secondary of SECONDARY_ACTIONS) {
      if (name.includes(secondary)) {
        return 'secondary'
      }
    }
  }

  if (node.type === 'MemberExpression' && !node.computed) {
    return classifyCalleeAction(node.property)
  }

  return null
}

function findFirstAction(node) {
  if (!node) {
    return null
  }

  if (node.type === 'CallExpression') {
    return classifyCalleeAction(node.callee)
  }

  if (node.type === 'BlockStatement') {
    for (const statement of node.body) {
      const hit = findFirstAction(statement)
      if (hit) {
        return hit
      }
    }
    return null
  }

  if (node.type === 'ExpressionStatement') {
    return findFirstAction(node.expression)
  }

  if (node.type === 'IfStatement') {
    return (
      findFirstAction(node.consequent) ||
      (node.alternate ? findFirstAction(node.alternate) : null)
    )
  }

  if (node.type === 'ReturnStatement') {
    return findFirstAction(node.argument)
  }

  return null
}

const AST_TRAVERSAL_SKIP_KEYS = new Set([
  'parent',
  'loc',
  'range',
  'start',
  'end',
  'comments',
  'leadingComments',
  'trailingComments',
  'tokens',
])

function collectJsxKeyViolations(handlerBody, keyCategory) {
  const violations = []
  const seen = new WeakSet()

  function walk(node) {
    if (!node || typeof node !== 'object') {
      return
    }

    if (Array.isArray(node)) {
      for (const child of node) {
        walk(child)
      }
      return
    }

    if (seen.has(node)) {
      return
    }
    seen.add(node)

    if (node.type === 'IfStatement') {
      const branchKey = extractKeyComparison(node.test)
      if (branchKey) {
        const action = findFirstAction(node.consequent)
        if (action) {
          violations.push({ action, key: branchKey, node: node.test })
        }
      }
      walk(node.consequent)
      if (node.alternate) {
        walk(node.alternate)
      }
      return
    }

    if (node.type === 'ConditionalExpression') {
      const branchKey = extractKeyComparison(node.test)
      if (branchKey) {
        const action =
          findFirstAction(node.consequent) || findFirstAction(node.alternate)
        if (action) {
          violations.push({ action, key: branchKey, node: node.test })
        }
      }
      walk(node.consequent)
      walk(node.alternate)
      return
    }

    if (node.type === 'LogicalExpression') {
      const branchKey = extractKeyComparison(node.left)
      if (branchKey) {
        const action = findFirstAction(node.right)
        if (action) {
          violations.push({ action, key: branchKey, node: node.left })
        }
      }
      walk(node.left)
      walk(node.right)
      return
    }

    for (const [key, value] of Object.entries(node)) {
      if (AST_TRAVERSAL_SKIP_KEYS.has(key)) {
        continue
      }
      if (value && typeof value === 'object') {
        walk(value)
      }
    }
  }

  walk(handlerBody)
  return violations.filter(({ key }) => !keyCategory || key === keyCategory)
}

function reportMismatch(context, key, action, node) {
  if (key === 'enter' && action !== 'primary') {
    context.report({ messageId: 'enter', node })
  }
  if (key === 'escape' && action !== 'secondary') {
    context.report({ messageId: 'escape', node })
  }
}

export const keyboardShortcutInvarianceRule = {
  meta: {
    docs: {
      description:
        'Enforce Enter as a primary action and Escape as a secondary action in keyboard shortcut registrations and JSX key handlers.',
    },
    messages: {
      escape:
        'Escape must map to a secondary/back/reject action so keyboard semantics stay consistent with the UX contract.',
      enter:
        'Enter must map to a primary/approve action so keyboard semantics stay consistent with the UX contract.',
    },
    schema: [],
    type: 'problem',
  },
  create(context) {
    return {
      ObjectExpression(node) {
        const key_property = getObjectProperty(node, ['key', 'shortcut'])
        const action_property = getObjectProperty(node, ['action', 'intent', 'semantic'])

        if (!key_property || !action_property) {
          return
        }

        const key_value = normalizeSemanticValue(readStaticString(key_property.value))
        const action_value = normalizeSemanticValue(
          readStaticString(action_property.value),
        )

        if (!key_value || !action_value) {
          return
        }

        if (ENTER_KEYS.has(key_value) && !PRIMARY_ACTIONS.has(action_value)) {
          context.report({
            messageId: 'enter',
            node: action_property.value,
          })
        }

        if (ESCAPE_KEYS.has(key_value) && !SECONDARY_ACTIONS.has(action_value)) {
          context.report({
            messageId: 'escape',
            node: action_property.value,
          })
        }
      },

      JSXAttribute(node) {
        if (node.name.type !== 'JSXIdentifier' || !JSX_KEY_ATTRIBUTES.has(node.name.name)) {
          return
        }
        if (!node.value || node.value.type !== 'JSXExpressionContainer') {
          return
        }
        const expression = node.value.expression
        if (
          !expression ||
          (expression.type !== 'ArrowFunctionExpression' &&
            expression.type !== 'FunctionExpression')
        ) {
          return
        }

        const violations = collectJsxKeyViolations(expression.body)
        for (const { action, key, node: violation_node } of violations) {
          reportMismatch(context, key, action, violation_node)
        }
      },
    }
  },
}
